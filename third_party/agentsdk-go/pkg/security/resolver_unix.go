//go:build !windows

package security

func supportsNoFollow() bool {
	return true
}
