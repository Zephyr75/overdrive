package utils

// Panics on a non-nil error, startup failures not being recoverable
func HandleError(err error) {
	if err != nil {
		panic(err)
	}
}
