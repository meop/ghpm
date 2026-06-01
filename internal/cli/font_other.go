//go:build !windows

package cli

func registerFont(_, _ string) error { return nil }
func unregisterFont(_ string)        {}
func fontRegistered(_ string) bool   { return true }
