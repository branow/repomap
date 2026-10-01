package catalog

// File is one tracked file of a repository.
type File struct {
	Path  string
	Lines int
	Bytes int64
	// Purpose is the generated phrase, present only for files whose purpose the
	// path does not already give away. Most rows carry none, on purpose: a
	// phrase restating the path is a phrase a reader has to read past.
	Purpose string
}

// Tree is everything a scan reads from one working tree: what the manifest
// cannot say without the files being there.
type Tree struct {
	// Head is the commit the scan read. It is recorded so a reader can tell
	// which revision a row describes, and never used to decide staleness - a
	// commit that changes no file the index renders is not a change.
	Head string
	// LastCommit is an ISO date, or empty when the repository has no commits.
	LastCommit string
	Branch     string
	// Readme is already truncated by the scanner; it is prompt input only and
	// never reaches the index.
	Readme string
	Files  []File
	Bytes  int64
}

// MaxReadme caps the README a scan carries. It lives here because the scanner
// that applies it and the prompt that spends it have to agree.
const MaxReadme = 3000

// Entry is one repository as the build carries it from scan to writer.
type Entry struct {
	Repo        Repo
	Tree        Tree
	Fingerprint string
	Description string
	// Skip names why a repository was never described, so the writer can say so
	// rather than leaving a row that looks like a failed describe.
	Skip string
}

func (e Entry) Key() string { return e.Repo.Key() }

// State is what the previous build recorded about one repository, read back out
// of the catalog that build wrote rather than from a sidecar file.
type State struct {
	// Remote is the clone url the previous build recorded, so a row carried
	// forward untouched keeps the address a reader needs.
	Remote      string
	Activity    Signal
	Fingerprint string
	Description string
	// Files are the per-file phrases the previous build wrote, keyed by path.
	// A repository whose fingerprint still matches keeps them rather than
	// buying them again.
	Files map[string]string
}
