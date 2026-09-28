package forge

import (
	"io"
	"net/http"
)

// maxBody caps one API response. A listing page is tens of kilobytes; a
// response far past that is a proxy's error page or a login form, and reading
// it whole into memory to then fail to parse it helps nobody.
const maxBody = 8 << 20

func readLimited(response *http.Response) ([]byte, error) {
	return io.ReadAll(io.LimitReader(response.Body, maxBody))
}
