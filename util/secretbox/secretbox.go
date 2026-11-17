package secretbox

import canonical "github.com/MalenkiySolovey/solovey-backend/security/secretbox"

const Prefix = canonical.Prefix

type Box = canonical.Box

func New(masterKey []byte) (*Box, error)     { return canonical.New(masterKey) }
func NewRawKey(key []byte) (*Box, error)     { return canonical.NewRawKey(key) }
func NewFromString(key string) (*Box, error) { return canonical.NewFromString(key) }
func IsEncrypted(value string) bool          { return canonical.IsEncrypted(value) }
