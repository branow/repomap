package clone

// AccessError reports a remote this machine cannot reach at all: the failure
// that is worth stopping a whole run for, because every repository after it
// would fail the same way.
type AccessError struct {
	Remote string
	Err    error
}

func (e *AccessError) Error() string {
	return "cannot reach " + e.Remote +
		": check the network, the VPN, and that this machine's git credentials still work"
}

func (e *AccessError) Unwrap() error { return e.Err }

// CloneError reports one repository that could not be cloned. It is per
// repository on purpose: a repository that is empty, or that this account
// cannot see, is one row the build skips, not a reason to abandon the rest.
type CloneError struct {
	Repo   string
	Remote string
	Err    error
}

func (e *CloneError) Error() string {
	return "cannot clone " + e.Repo + " from " + e.Remote + ": " + e.Err.Error()
}

func (e *CloneError) Unwrap() error { return e.Err }

// UpdateError reports an existing clone that could not be refreshed.
type UpdateError struct {
	Repo string
	Dir  string
	Err  error
}

func (e *UpdateError) Error() string {
	return "cannot update the clone of " + e.Repo + " at " + e.Dir + ": " + e.Err.Error()
}

func (e *UpdateError) Unwrap() error { return e.Err }

// NoRemoteError reports a repository with neither a directory on disk nor a
// remote to clone from, which is a forge reporting something this tool cannot
// read.
type NoRemoteError struct{ Repo string }

func (e *NoRemoteError) Error() string {
	return e.Repo + " has no clone url and no directory on this machine"
}
