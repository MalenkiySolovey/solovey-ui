//go:build !linux

package openwrt

func LoadStorageSelection() (StorageSelection, error) { return StorageSelection{}, nil }
