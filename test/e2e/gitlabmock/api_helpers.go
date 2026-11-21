package gitlabmock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

func writeJSON(writer http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)

		return
	}

	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)

	sendBytes(writer, append(body, '\n'))
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, map[string]string{"message": message})
}

// paginate returns the page of items the request asks for, the way GitLab does,
// and sets the pagination headers.
func paginate[T any](writer http.ResponseWriter, request *http.Request, items []T) []T {
	total := len(items)

	perPage := atoiDefault(request.URL.Query().Get("per_page"), defaultPerPage)
	if perPage < 1 || perPage > maxPerPage {
		perPage = maxPerPage
	}

	page := max(atoiDefault(request.URL.Query().Get("page"), 1), 1)
	totalPages := max((total+perPage-1)/perPage, 1)

	header := writer.Header()
	header.Set("X-Total", strconv.Itoa(total))
	header.Set("X-Total-Pages", strconv.Itoa(totalPages))
	header.Set("X-Per-Page", strconv.Itoa(perPage))
	header.Set("X-Page", strconv.Itoa(page))

	if page < totalPages {
		header.Set("X-Next-Page", strconv.Itoa(page+1))
	} else {
		header.Set("X-Next-Page", "")
	}

	start := min((page-1)*perPage, total)
	end := min(start+perPage, total)

	return items[start:end]
}

// pathInt64 parses a numeric value of the request path. A malformed one gives 0,
// which matches no entity (the instance answers 404, as GitLab does).
func pathInt64(request *http.Request, name string) int64 {
	value, err := strconv.ParseInt(request.PathValue(name), 10, 64)
	if err != nil {
		return 0
	}

	return value
}

func atoiDefault(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}

	return value
}

// decodeBody decodes a JSON request body into target. An empty body is fine.
func decodeBody(request *http.Request, target any) error {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return fmt.Errorf("read the request body: %w", err)
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	err = json.Unmarshal(body, target)
	if err != nil {
		return fmt.Errorf("decode the request body: %w", err)
	}

	return nil
}
