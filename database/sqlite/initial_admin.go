package sqlite

import canonical "github.com/MalenkiySolovey/solovey-ui/backend/storage/bootstrap"

const initialAdminPasswordFile = canonical.InitialAdminPasswordFile

func initialAdminPasswordPath(path string) string {
	return storageInitializer().InitialAdminPasswordPath(path)
}
func RemoveInitialAdminPasswordFile() error {
	return storageInitializer().RemoveInitialAdminPasswordFile()
}
