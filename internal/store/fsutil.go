package store

import "os"

func mkdirAll(dir string) error { return os.MkdirAll(dir, 0o750) }
