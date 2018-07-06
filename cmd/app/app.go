package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	internalErrors "github.com/SprintHubNigeria/nearbuy-images/pkg/errors"

	"google.golang.org/appengine/file"
	"google.golang.org/appengine/taskqueue"

	"github.com/SprintHubNigeria/nearbuy-images/pkg/image"
	"google.golang.org/appengine/urlfetch"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
)

const (
	externalImageURL = "externalImageUrl"
	productID        = "productId"
)

var (
	bucketName             = os.Getenv("GCS_STORAGE_BUCKET")
	productImagesDirectory = os.Getenv("PRODUCT_IMAGES_DIR")
	once                   = sync.Once{}
)

func main() {
	http.HandleFunc("/_ah/warmup", warmUp)
	http.HandleFunc("/servingURL", func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			ensureEnvVars(appengine.NewContext(r))
		})
		if r.Method == http.MethodDelete {
			deleteServingURL(w, r)
		} else if r.Method == http.MethodGet {
			routeGetServingURL(w, r)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
		return
	})
	appengine.Main()
}

func warmUp(w http.ResponseWriter, r *http.Request) {
	ensureEnvVars(appengine.NewContext(r))
}

func routeGetServingURL(w http.ResponseWriter, r *http.Request) {
	product, externalURL := extractQueryParams(r)
	if ok, reason := requireQueryParams(w, product, externalURL); !ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(reason))
		return
	}
	ctx := appengine.NewContext(r)
	var (
		servingURL string
		err        error
	)
	if strings.HasPrefix(externalURL, "http") {
		if r.Header.Get("X-AppEngine-QueueName") == "" {
			if err := sendToTaskQueue(ctx, product, externalURL); err != nil {
				log.Criticalf(ctx, "%+v\n", err)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Could not enqueue task"))
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(""))
			return
		}
		servingURL, err = getServingURLExternal(ctx, product, externalURL)
	} else {
		servingURL, err = getServingURLFromGCS(ctx, externalURL)
	}
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(servingURL))
	return
}

func extractQueryParams(r *http.Request) (product, externalURL string) {
	query := r.URL.Query()
	return query.Get(productID), query.Get(externalImageURL)
}

func requireQueryParams(w http.ResponseWriter, product, externalImageURL string) (bool, string) {
	if product == "" {
		return false, "No product ID in request query"
	}
	if externalImageURL == "" {
		return false, "No external URL in request query"
	}
	return true, ""
}

func sendToTaskQueue(ctx context.Context, product, externalURL string) error {
	params := url.Values{}
	params.Add(productID, product)
	params.Add(externalImageURL, externalURL)
	task := taskqueue.Task{
		Method:       http.MethodGet,
		Path:         "/servingURL?" + params.Encode(),
		RetryOptions: &taskqueue.RetryOptions{RetryLimit: 2, MinBackoff: time.Duration(2 * time.Second)},
	}
	if _, err := taskqueue.Add(ctx, &task, "external-image-urls"); err != nil {
		return err
	}
	return nil
}

func getServingURLExternal(ctx context.Context, fileName, externalURL string) (string, error) {
	client := urlfetch.Client(ctx)
	img, err := image.DownloadImage(ctx, client, externalURL, relativeFilePath(fileName))
	if err != nil {
		log.Criticalf(ctx, "%+v\n", err)
		return "", internalErrors.ErrImageDownloadFailed
	}
	if err := img.SaveToGCS(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "%+v\n", err)
		return "", internalErrors.ErrImageSaveFailed
	}
	servingURL, err := img.CreateServingURL(ctx, bucketName)
	if err != nil {
		log.Criticalf(ctx, "%+v\n", err)
		return "", internalErrors.ErrMakeServingURLFailed
	}
	return servingURL, nil
}

func getServingURLFromGCS(ctx context.Context, gcsFileName string) (string, error) {
	img := &image.Image{FileName: gcsFileName}
	return img.CreateServingURL(ctx, bucketName)
}

func deleteServingURL(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	product := query.Get(productID)
	if product == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No product ID in request query\n"))
		return
	}
	ctx := appengine.NewContext(r)
	img := &image.Image{FileName: relativeFilePath(product)}
	if err := img.DeleteServingURL(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "Deleting serving URL failed with error: %+v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Could not delete serving URL for image %s, please retry\n", product)))
		return
	}
	if err := img.DeleteFromGCS(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "Deleting image failed with error: %+v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Could not delete image %s, please retry\n", product)))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Image and serving URL deleted\n"))
}

func relativeFilePath(fileName string) string {
	return fmt.Sprintf("%s/%s", strings.Trim(productImagesDirectory, "/"), fileName)
}

func ensureEnvVars(ctx context.Context) {
	if productImagesDirectory == "" {
		panic("Missing environment variable PRODUCT_IMAGES_DIRECTORY")
	}
	if bucketName == "" {
		var err error
		bucketName, err = file.DefaultBucketName(ctx)
		if err != nil {
			panic(err)
		}
	}
}
