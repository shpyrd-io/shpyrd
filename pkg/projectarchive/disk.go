package projectarchive

// DiskCapacity is the space of the disk a volume lives on, as the archive
// pod reads it before a restore.
type DiskCapacity struct {
	Available uint64 `json:"available"`
	Total     uint64 `json:"total"`
}
