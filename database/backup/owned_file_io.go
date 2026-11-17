package backup

import canonical "github.com/MalenkiySolovey/solovey-backend/storage/backup"

func ReadOwnedFile(root, name string) ([]byte, error) { return canonical.ReadOwnedFile(root, name) }
func PublishOwnedFile(root string, data []byte, publish bool) (string, error) {
	return canonical.PublishOwnedFile(root, data, publish)
}
func PublishOwnedTree(root string, files map[string][]byte, publish bool) (string, error) {
	return canonical.PublishOwnedTree(root, files, publish)
}
func syncRestoreRecoveryDirectory(path string) error { return canonical.SyncRecoveryDirectory(path) }
