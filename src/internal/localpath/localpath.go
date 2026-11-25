// Package localpath tells a path on a local drive from a network share or a
// device namespace path, which only Windows paths can name.
package localpath

import "path/filepath"

// IsLocal reports whether p names a location on a local drive: it has no
// volume, or a drive letter volume such as C:. A UNC share (\\host\share,
// //host/share) or a device path (\\?\..., \\.\...) is not local. On systems
// other than Windows every path is local.
func IsLocal(p string) bool {
	return !nonDriveVolume(filepath.VolumeName(p))
}

// nonDriveVolume reports whether vol, as filepath.VolumeName returns it, is a
// volume other than a drive letter.
func nonDriveVolume(vol string) bool {
	return vol != "" && (len(vol) != 2 || vol[1] != ':')
}
