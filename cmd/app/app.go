package main

import (
	"context"
	"fmt"
	"net/http"
	"os"

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
)

func main() {
	if bucketName == "" {
		panic("Missing environment variable GCS_STORAGE_BUCKET")
	}
	if productImagesDirectory == "" {
		panic("Missing environment variable PRODUCT_IMAGES_DIRECTORY")
	}
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

func getServingURL(w http.ResponseWriter, r *http.Request) {
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
	ctx := context.WithValue(appengine.NewContext(r), image.FileName, fmt.Sprintf("%s/%s", productImagesDirectory, product))
	client := urlfetch.Client(ctx)
	img, err := image.DownloadImage(ctx, client, externalURL)
	if err != nil {
		log.Criticalf(ctx, "image.DownloadImage failed with error: %v\n", err)
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("Could not fetch image at URL %s\n", externalURL)))
		return
	}
	if err := img.SaveToGCS(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "img.SaveToGCS failed with error: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Could not save image to cloud storage\n"))
		return
	}
	servingURL, err := img.CreateServingURL(ctx, bucketName)
	if err != nil {
		log.Criticalf(ctx, "img.ServingURL failed with error: %v\n", err)
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
	ctx := context.WithValue(appengine.NewContext(r), image.FileName, fmt.Sprintf("%s/%s", productImagesDirectory, product))
	img := &image.Image{FileName: product}
	if err := img.DeleteServingURL(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "Deleting serving URL failed with error: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Could not delete serving URL for image %s, please retry\n", product)))
		return
	}
	if err := img.DeleteFromGCS(ctx, bucketName); err != nil {
		log.Criticalf(ctx, "Deleting image failed with error: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Could not delete image %s, please retry\n", product)))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Image and serving URL deleted\n"))
}
