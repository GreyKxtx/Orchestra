package view

// Component is the small interface every visible TUI element satisfies:
// it knows its width (some know height too) and renders itself.
//
// Concrete types still expose their own typed APIs (SetSize variants,
// SetMode, SetProject, …); this interface only exists so app-level code
// that needs to treat the chrome as a list of "size + render" surfaces
// can do so without per-type duck typing.
type Component interface {
	Render() string
}

// SizedComponent is a Component that takes a width on resize. Implemented
// by StatusBar, Input — components that live in a flex row and only react
// to width changes, not height.
type SizedComponent interface {
	Component
	SetSize(width int)
}
