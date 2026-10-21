package types

// VPNUserID and PanelAccountID are distinct adapters over existing local storage
// identities. They allocate no IDs and are not cross-Host transport identities.
// Names, subscription tokens and network observations cannot mint either ID.
type VPNUserID uint
type PanelAccountID uint
