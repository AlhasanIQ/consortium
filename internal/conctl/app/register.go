package app

// Register adds a resource to the app.
func (a *App) Register(r *Resource) {
	a.resources = append(a.resources, r)
}
