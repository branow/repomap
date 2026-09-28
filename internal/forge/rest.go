package forge

import (
	"context"
	"encoding/json"
	"net/http"
)

// Page is one page of a paginated listing: the rows it carried, and where the
// next page is. Both REST forges here paginate, differing only in how they say
// so - one in the body, one in a Link header - which is the one thing each
// decode function has to answer. It is handed both, so neither forge needs its
// own request loop.
type Page[T any] struct {
	Items []T
	Next  string
}

// Paginate walks a paginated endpoint to exhaustion, calling decode on each
// response. It is the shared half of every REST forge: the request, the status
// mapping and the loop are identical, and only the response shape is not.
//
// Every page is fetched before anything is described, because a listing that
// stopped halfway would silently index a subset - and a subset is
// indistinguishable, in the tree it writes, from an account that shrank.
func Paginate[T any](ctx context.Context, cfg Config, name, slug, first string,
	prepare func(*http.Request), decode func([]byte, http.Header) (Page[T], error)) ([]T, error) {

	var all []T
	url := first
	// A cap on pages rather than on items: an endpoint answering with a next
	// link that points at itself is a bug that would otherwise loop forever.
	for pages := 0; url != "" && pages < maxPages; pages++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, &UnavailableError{Forge: name, Err: err}
		}
		request.Header.Set("Accept", "application/json")
		if prepare != nil {
			prepare(request)
		}
		if cfg.Credential.Present() {
			if cfg.Username != "" {
				request.SetBasicAuth(cfg.Username, cfg.Token)
			} else {
				request.Header.Set("Authorization", "Bearer "+cfg.Token)
			}
		}

		response, err := cfg.Client().Do(request)
		if err != nil {
			return nil, &UnavailableError{Forge: name, Err: err}
		}
		body, err := readAll(response)
		if err != nil {
			return nil, &UnavailableError{Forge: name, Err: err}
		}
		if err := Classify(name, slug, url, response.StatusCode); err != nil {
			return nil, err
		}

		page, err := decode(body, response.Header)
		if err != nil {
			return nil, &ResponseError{Forge: name, URL: url, Err: err}
		}
		all = append(all, page.Items...)
		url = page.Next
	}
	return all, nil
}

// maxPages bounds a listing walk. At the page sizes both forges use this is far
// more repositories than any account has, so reaching it means the endpoint is
// looping, not that the account is large.
const maxPages = 500

func readAll(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	return readLimited(response)
}

// Unmarshal is the decode half every forge shares: JSON into the forge's own
// page shape.
func Unmarshal(body []byte, into any) error { return json.Unmarshal(body, into) }
