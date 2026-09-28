package config

import "sort"

// Workspace returns a named workspace.
func (c *Config) Workspace(name string) (Workspace, error) {
	entry, ok := c.Workspaces[name]
	if !ok {
		return Workspace{}, &NotFoundError{Kind: KindWorkspace, Name: name}
	}
	return entry, nil
}

// Backend returns a named backend.
func (c *Config) Backend(name string) (Backend, error) {
	entry, ok := c.Backends[name]
	if !ok {
		return Backend{}, &NotFoundError{Kind: KindBackend, Name: name}
	}
	return entry, nil
}

// Profile returns a named profile.
func (c *Config) Profile(name string) (Profile, error) {
	entry, ok := c.Profiles[name]
	if !ok {
		return Profile{}, &NotFoundError{Kind: KindProfile, Name: name}
	}
	return entry, nil
}

// SetWorkspace defines or redefines a workspace; defining never activates it.
func (c *Config) SetWorkspace(name string, entry Workspace) error {
	if err := validateWorkspace(name, entry); err != nil {
		return err
	}
	c.normalise()
	c.Workspaces[name] = entry
	return nil
}

// SetBackend defines or redefines a backend, without activating it.
func (c *Config) SetBackend(name string, entry Backend) error {
	if err := validateBackend(name, entry); err != nil {
		return err
	}
	c.normalise()
	c.Backends[name] = entry
	return nil
}

// SetProfile defines or redefines a profile; it does not become current.
func (c *Config) SetProfile(name string, entry Profile) error {
	if err := validateName(KindProfile, name); err != nil {
		return err
	}
	if _, err := c.Workspace(entry.Workspace); err != nil {
		return err
	}
	if _, err := c.Backend(entry.Backend); err != nil {
		return err
	}
	c.normalise()
	c.Profiles[name] = entry
	return nil
}

// RemoveWorkspace refuses while a profile still binds it, unless forced.
func (c *Config) RemoveWorkspace(name string, force bool) error {
	if _, err := c.Workspace(name); err != nil {
		return err
	}
	if users := c.profilesUsing(KindWorkspace, name); len(users) > 0 && !force {
		return &InUseError{Kind: KindWorkspace, Name: name, By: users}
	}
	delete(c.Workspaces, name)
	return nil
}

// RemoveBackend drops a backend, with the same in-use rule as a workspace.
func (c *Config) RemoveBackend(name string, force bool) error {
	if _, err := c.Backend(name); err != nil {
		return err
	}
	if users := c.profilesUsing(KindBackend, name); len(users) > 0 && !force {
		return &InUseError{Kind: KindBackend, Name: name, By: users}
	}
	delete(c.Backends, name)
	return nil
}

// RemoveProfile drops a profile and clears the current one when it was it.
func (c *Config) RemoveProfile(name string) error {
	if _, err := c.Profile(name); err != nil {
		return err
	}
	delete(c.Profiles, name)
	if c.CurrentProfile == name {
		c.CurrentProfile = ""
	}
	return nil
}

// Switch makes a profile current. It is the only operation that does.
func (c *Config) Switch(name string) error {
	if _, err := c.Profile(name); err != nil {
		return err
	}
	c.CurrentProfile = name
	return nil
}

// WorkspaceNames lists defined workspaces in stable order.
func (c *Config) WorkspaceNames() []string { return sortedKeys(c.Workspaces) }

// BackendNames lists defined backends in stable order.
func (c *Config) BackendNames() []string { return sortedKeys(c.Backends) }

// ProfileNames lists defined profiles in stable order.
func (c *Config) ProfileNames() []string { return sortedKeys(c.Profiles) }

// profilesUsing reports which profiles bind a given entry.
func (c *Config) profilesUsing(kind Kind, name string) []string {
	var users []string
	for profile, entry := range c.Profiles {
		bound := entry.Workspace
		if kind == KindBackend {
			bound = entry.Backend
		}
		if bound == name {
			users = append(users, profile)
		}
	}
	sort.Strings(users)
	return users
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
