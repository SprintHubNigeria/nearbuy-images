package image_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SprintHubNigeria/nearbuy-images/pkg/image"
	"github.com/stretchr/testify/assert"
)

func TestDownloadImage(t *testing.T) {
	client := http.Client{}
	srv := httptest.NewServer(http.StripPrefix("/image/", http.FileServer(http.Dir("../../testdata/image"))))
	defer srv.Close()
	srvUrl := srv.URL
	tt := []struct {
		testName  string
		testURL   string
		testCtx   context.Context
		fileName  string
		assertion func(*testing.T, *image.Image, error)
	}{
		{
			"Downloading from a broken URL",
			fmt.Sprintf("%s/image/image.png", srvUrl),
			context.Background(),
			"image.png",
			func(t *testing.T, img *image.Image, err error) {
				assert.Nil(t, img, "Should return an empty byte slice")
				assert.Error(t, err, "Should also returns an error")
			},
		},
		{
			"Downloading from a non-broken URL",
			fmt.Sprintf("%s/image/sunset.jpg", srvUrl),
			context.Background(),
			"sunset.png",
			func(t *testing.T, img *image.Image, err error) {
				assert.NotNil(t, img, "Should return the image data")
				assert.Nil(t, err, "Should not return an error")
			},
		},
		{
			"Passing an empty file name",
			fmt.Sprintf("%s/image/sunset.jpg", srvUrl),
			context.Background(),
			"",
			func(t *testing.T, img *image.Image, err error) {
				assert.Nil(t, img, "Should not return any image data")
				assert.Error(t, err, "Should return an error")
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.testName, func(t *testing.T) {
			img, err := image.DownloadImage(tc.testCtx, &client, tc.testURL, tc.fileName)
			tc.assertion(t, img, err)
		})
	}
}
