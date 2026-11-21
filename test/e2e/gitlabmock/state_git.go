package gitlabmock

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// commitSignature is the author and committer of the seeded commits.
func commitSignature() object.Signature {
	return object.Signature{Name: "Seed", Email: "seed@example.com", When: time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)}
}

// repo opens (creating it when needed) the bare repository of a project.
func (p *project) repo() (*git.Repository, error) {
	repository, err := git.PlainOpen(p.repoDir)
	if err == nil {
		return repository, nil
	}

	if !errors.Is(err, git.ErrRepositoryNotExists) {
		return nil, fmt.Errorf("open the repository of %s: %w", p.PathWithNamespace, err)
	}

	repository, err = git.PlainInit(p.repoDir, true)
	if err != nil {
		return nil, fmt.Errorf("create the repository of %s: %w", p.PathWithNamespace, err)
	}

	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(p.DefaultBranch))

	err = repository.Storer.SetReference(head)
	if err != nil {
		return nil, fmt.Errorf("point HEAD of %s to %s: %w", p.PathWithNamespace, p.DefaultBranch, err)
	}

	return repository, nil
}

// writeBlob stores content as a blob.
func writeBlob(repository *git.Repository, content string) (plumbing.Hash, error) {
	blob := repository.Storer.NewEncodedObject()
	blob.SetType(plumbing.BlobObject)

	writer, err := blob.Writer()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("open the blob: %w", err)
	}

	_, err = writer.Write([]byte(content))
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("write the blob: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("close the blob: %w", err)
	}

	hash, err := repository.Storer.SetEncodedObject(blob)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store the blob: %w", err)
	}

	return hash, nil
}

// writeTree stores a tree holding the one file, whose content is the blob.
func writeTree(repository *git.Repository, file string, blob plumbing.Hash) (plumbing.Hash, error) {
	tree := &object.Tree{Entries: []object.TreeEntry{{Name: file, Mode: filemode.Regular, Hash: blob}}}
	encoded := repository.Storer.NewEncodedObject()

	err := tree.Encode(encoded)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode the tree: %w", err)
	}

	hash, err := repository.Storer.SetEncodedObject(encoded)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store the tree: %w", err)
	}

	return hash, nil
}

// writeCommit stores a commit of the tree, on top of the parents.
func writeCommit(repository *git.Repository, file string, tree plumbing.Hash, parents []plumbing.Hash) (plumbing.Hash, error) {
	// The time moves with the history and the content, so that commits differ.
	signature := commitSignature()
	signature.When = signature.When.Add(time.Duration(len(parents)+int(tree[0])) * time.Second)

	commit := &object.Commit{Author: signature, Committer: signature, Message: "update " + file, TreeHash: tree, ParentHashes: parents}
	encoded := repository.Storer.NewEncodedObject()

	err := commit.Encode(encoded)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encode the commit: %w", err)
	}

	hash, err := repository.Storer.SetEncodedObject(encoded)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store the commit: %w", err)
	}

	return hash, nil
}

// Commit adds a commit writing content to file on branch of the project at
// fullPath, creating the branch when missing. It returns the commit hash.
func (s *Server) Commit(fullPath, branch, file, content string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return "", fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return "", err
	}

	var parents []plumbing.Hash

	branchRef := plumbing.NewBranchReferenceName(branch)

	ref, refErr := repository.Reference(branchRef, true)
	if refErr == nil {
		parents = append(parents, ref.Hash())
	}

	blob, err := writeBlob(repository, content)
	if err != nil {
		return "", err
	}

	tree, err := writeTree(repository, file, blob)
	if err != nil {
		return "", err
	}

	commit, err := writeCommit(repository, file, tree, parents)
	if err != nil {
		return "", err
	}

	err = repository.Storer.SetReference(plumbing.NewHashReference(branchRef, commit))
	if err != nil {
		return "", fmt.Errorf("move %s of %s: %w", branch, fullPath, err)
	}

	target.EmptyRepo = false

	return commit.String(), nil
}

// Tag creates the lightweight tag name on the tip of branch.
func (s *Server) Tag(fullPath, name, branch string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return err
	}

	ref, err := repository.Reference(plumbing.NewBranchReferenceName(branch), true)
	if err != nil {
		return fmt.Errorf("branch %s of %s: %w", branch, fullPath, err)
	}

	err = repository.Storer.SetReference(plumbing.NewHashReference(plumbing.NewTagReferenceName(name), ref.Hash()))
	if err != nil {
		return fmt.Errorf("create the tag %s of %s: %w", name, fullPath, err)
	}

	return nil
}

// Refs returns the branches and tags of a project's repository ("refs/heads/main"
// -> commit hash).
func (s *Server) Refs(fullPath string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return nil, fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return nil, err
	}

	iterator, err := repository.References()
	if err != nil {
		return nil, fmt.Errorf("list the references of %s: %w", fullPath, err)
	}

	refs := make(map[string]string)

	err = iterator.ForEach(func(ref *plumbing.Reference) error {
		if ref.Type() == plumbing.HashReference {
			refs[ref.Name().String()] = ref.Hash().String()
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk the references of %s: %w", fullPath, err)
	}

	return refs, nil
}

// DeleteRef removes a branch or a tag ("refs/heads/dev") of a project's repository.
func (s *Server) DeleteRef(fullPath, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.projectByPath(fullPath)
	if target == nil {
		return fmt.Errorf("project %s: %w", fullPath, errNotFound)
	}

	repository, err := target.repo()
	if err != nil {
		return err
	}

	err = repository.Storer.RemoveReference(plumbing.ReferenceName(ref))
	if err != nil {
		return fmt.Errorf("delete %s of %s: %w", ref, fullPath, err)
	}

	return nil
}
