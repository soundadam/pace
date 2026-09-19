// This file is a local addition to the LibreSpeed CLI source maintained in
// this repository. It is covered by the same GNU LGPL v3 license as the rest
// of the directory (see ../LICENSE).
//
// Redirect exists so the newline-delimited `--progress-json` event stream,
// which is a wire contract consumed by another process, can be asserted on
// in-process by tests without spawning the helper binary. The package-level
// Default writer captures os.Stdout/os.Stderr at initialisation, so swapping
// the process-wide files afterwards is not observable here.

package output

import "io"

// Redirect points the package-level Default writer at out (the stdout role,
// machine-readable data) and ui (the stderr role, human-readable UI, debug and
// error output). It returns a function that restores the previous writers.
//
// Redirect is not safe for concurrent use; callers are expected to restore the
// previous writers before handing control back.
func Redirect(out, ui io.Writer) func() {
	previousOut, previousUI := Default.out, Default.ui
	Default.out, Default.ui = out, ui
	return func() {
		Default.out, Default.ui = previousOut, previousUI
	}
}
