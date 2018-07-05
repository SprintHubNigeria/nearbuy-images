package image

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"google.golang.org/appengine/blobstore"
	"google.golang.org/appengine/image"

	"golang.org/x/net/context"

	"cloud.google.com/go/storage"
)

var allowedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpg":  true,
	"image/jpeg": true,
}

// Image holds data about images stored in GCS
type Image struct {
	FileName    string
	OriginalURL string
	ServingURL  string
	Data        []byte
	ContentType string
}

type filename string

// FileName is a context key for the image file name
var FileName = filename("fileName")

// DownloadImage fetches the image from the source url
func DownloadImage(ctx context.Context, client *http.Client, url string) (*Image, error) {
	f := ctx.Value(FileName)
	fileName, ok := f.(string)
	if !ok {
		return nil, fmt.Errorf("Cannot use filename: %q of type %T as string", fileName, f)
	}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Image not found")
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &Image{
		FileName:    fileName,
		ContentType: resp.Header.Get("Content-Type"),
		Data:        b,
		OriginalURL: url,
	}, nil
}

// SaveToGCS saves the image downloaded to cloud storage
func (img *Image) SaveToGCS(ctx context.Context, bucketName string) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	wc := client.Bucket(bucketName).Object(img.FileName).NewWriter(ctx)
	wc.ContentType = "image/jpeg"
	wc.Metadata = map[string]string{
		"x-goog-meta-source": img.OriginalURL,
	}
	if _, err := wc.Write(img.Data); err != nil {
		return fmt.Errorf("Could not write image to file %q: %v", img.FileName, err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("Could not close bucket %q, file %q: %v", bucketName, img.FileName, err)
	}
	return nil
}

// CreateServingURL returns a serving URL for an image in cloud storage
func (img *Image) CreateServingURL(ctx context.Context, bucketName string) (string, error) {
	blobKey, err := blobstore.BlobKeyForFile(ctx, "/gs/"+bucketName+img.FileName)
	if err != nil {
		return "", err
	}
	servingURL, err := image.ServingURL(ctx, blobKey, &image.ServingURLOptions{
		Secure: true,
		Size:   450,
	})
	if err != nil {
		return "", err
	}
	img.ServingURL = servingURL.String()
	return img.ServingURL, nil
}

// DeleteServingURL makes the serving URL unavailable
func (img *Image) DeleteServingURL(ctx context.Context, bucketName string) error {
	key, err := blobstore.BlobKeyForFile(ctx, "/gs/"+bucketName+img.FileName)
	if err != nil {
		return err
	}
	return image.DeleteServingURL(ctx, key)
}

// DeleteFromGCS removes the image from cloud storage
func (img *Image) DeleteFromGCS(ctx context.Context, bucketName string) error {
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	return client.Bucket(bucketName).Object(img.FileName).Delete(ctx)
}
