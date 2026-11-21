//go:build !windows

package backend

func hideWithACL(path string) error {
	return errWindowsOnly
}

func showWithACL(path string) (string, error) {
	return "", errWindowsOnly
}
