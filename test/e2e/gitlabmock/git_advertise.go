package gitlabmock

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// errUnknownService is returned for a git service the fake does not implement.
var errUnknownService = errors.New("unknown git service")

// advertise returns the references the service advertises (none for an empty
// repository).
func advertise(ctx context.Context, backend transport.Transport, endpoint *transport.Endpoint, service string) (*packp.AdvRefs, error) {
	switch service {
	case uploadPack:
		return advertiseUploadPack(ctx, backend, endpoint)
	case receivePack:
		return advertiseReceivePack(ctx, backend, endpoint)
	default:
		return nil, errUnknownService
	}
}

func advertiseUploadPack(ctx context.Context, backend transport.Transport, endpoint *transport.Endpoint) (*packp.AdvRefs, error) {
	session, err := backend.NewUploadPackSession(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("open the upload-pack session: %w", err)
	}

	defer closeLogged(session)

	refs, err := session.AdvertisedReferencesContext(ctx)
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return refs, nil
	}

	if err != nil {
		return nil, fmt.Errorf("advertise the references: %w", err)
	}

	return refs, nil
}

func advertiseReceivePack(ctx context.Context, backend transport.Transport, endpoint *transport.Endpoint) (*packp.AdvRefs, error) {
	session, err := backend.NewReceivePackSession(endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("open the receive-pack session: %w", err)
	}

	defer closeLogged(session)

	refs, err := session.AdvertisedReferencesContext(ctx)
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return refs, nil
	}

	if err != nil {
		return nil, fmt.Errorf("advertise the references: %w", err)
	}

	err = refs.Capabilities.Set(pushOptionsCapability)
	if err != nil {
		return nil, fmt.Errorf("advertise the push options: %w", err)
	}

	return refs, nil
}
