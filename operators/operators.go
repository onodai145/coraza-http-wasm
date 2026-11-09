//go:build tinygo

package operators

// Register keeps Coraza's built-in operators. Precompiled WASI C/C++ operator
// archives require a libc ABI that TinyGo 0.43 no longer provides. The built-in
// Coraza operators support the same rule API without external host imports.
func Register() {}
