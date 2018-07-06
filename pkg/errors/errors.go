package errors

import (
	"errors"
)

var (
	// ErrImageDownloadFailed is returned for image download failures
	ErrImageDownloadFailed = errors.New("Image download failed")
	// ErrImageSaveFailed is returned if saving image to Cloud Storage fails
	ErrImageSaveFailed = errors.New("Saving image to storage failed")
	// ErrMakeServingURLFailed is returned if the serving URL could not be created
	ErrMakeServingURLFailed = errors.New("Could not get serving URL")
)
