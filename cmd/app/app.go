package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"google.golang.org/appengine/file"

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
		if r.Method == http.MethodDelete {
			deleteServingURL(w, r)
		} else if r.Method == http.MethodGet {
			getServingURL(w, r)
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

func getServingURL(w http.ResponseWriter, r *http.Request) {
	once.Do(func() {
		ensureEnvVars(appengine.NewContext(r))
	})
	query := r.URL.Query()
	product := query.Get(productID)
	if product == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No product ID in request query\n"))
		return
	}
	externalURL := query.Get(externalImageURL)
	if externalURL == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("No external URL in request query\n"))
		return
	}
	ctx := appengine.NewContext(r)
	client := urlfetch.Client(ctx)
	img, err := image.DownloadImage(ctx, client, externalURL, relativeFilePath(product))
	if err != nil {
		log.Criticalf(ctx, "image.DownloadImage failed with error: %+v\n", err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("Could not fetch image at URL %s\n", externalURL)))
		return
	}
	if err := img.SaveToGCS(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "img.SaveToGCS failed with error: %+v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not save image to cloud storage\n"))
		return
	}
	servingURL, err := img.CreateServingURL(ctx, bucketName)
	if err != nil {
		log.Criticalf(ctx, "img.ServingURL failed with error: %+v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not get serving URL, please retry\n"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(servingURL))
	return
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
	return fmt.Sprintf("%s/%s", productImagesDirectory, fileName)
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
