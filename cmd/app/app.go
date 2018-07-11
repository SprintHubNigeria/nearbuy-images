package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	// MySQL driver for database connection
	_ "github.com/go-sql-driver/mysql"

	internalErrors "github.com/SprintHubNigeria/nearbuy-images/pkg/errors"

	"google.golang.org/appengine/file"
	"google.golang.org/appengine/taskqueue"

	"github.com/SprintHubNigeria/nearbuy-images/pkg/image"
	"google.golang.org/appengine/urlfetch"

	"google.golang.org/appengine"
	"google.golang.org/appengine/log"
)

const (
	externalImageURL = "externalImageURL"
	productID        = "productID"
	callbackURL      = "callbackURL"
)

var (
	bucketName             = os.Getenv("GCS_STORAGE_BUCKET")
	productImagesDirectory = os.Getenv("PRODUCT_IMAGES_DIR")
	once                   = sync.Once{}
	mysqlURL               = os.Getenv("MYSQL_DATABASE_URL")
	db                     *sql.DB
)

func main() {
	var err error
	db, err = sql.Open("mysql", mysqlURL)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	if err = db.Ping(); err != nil {
		panic(err)
	}
	http.HandleFunc("/_ah/warmup", warmUp)
	http.HandleFunc("/servingURLExternal", servingURLExternal)
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
	w.WriteHeader(http.StatusOK)
	return
}

type queryParams struct {
	imageName string
	imageURL  string
}

func makeQueryParams(r *http.Request) *queryParams {
	query := r.URL.Query()
	return &queryParams{
		imageName: query.Get(productID),
		imageURL:  query.Get(externalImageURL),
	}
}

func routeGetServingURL(w http.ResponseWriter, r *http.Request) {
	query := makeQueryParams(r)
	ctx := appengine.NewContext(r)
	if strings.HasPrefix(query.imageURL, "http") {
		if query.imageURL == "" || query.imageName == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(""))
			return
		}
		if err := sendToTaskQueue(ctx, query); err != nil {
			log.Criticalf(ctx, "%+v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("Could not enqueue task"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(""))
		return
	}
	if query.imageURL == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(""))
		return
	}
	servingURL, err := makeServingURLFromGCS(ctx, query.imageURL)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(err.Error()))
		return
	}
	w.Write([]byte(servingURL))
	return
}

func servingURLExternal(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-AppEngine-QueueName") == "" {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(""))
		return
	}
	query := makeQueryParams(r)
	ctx := appengine.NewContext(r)
	servingURL, err := makeServingURLFromExternal(ctx, query.imageName, query.imageURL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Write([]byte(servingURL))
	return
}

func sendToTaskQueue(ctx context.Context, query *queryParams) error {
	params := url.Values{}
	params.Add(productID, query.imageName)
	params.Add(externalImageURL, query.imageURL)
	task := taskqueue.Task{
		Method:       http.MethodGet,
		Path:         "/servingURLExternal?" + params.Encode(),
		RetryOptions: &taskqueue.RetryOptions{RetryLimit: 2, MinBackoff: time.Duration(2 * time.Second)},
	}
	if _, err := taskqueue.Add(ctx, &task, "external-image-urls"); err != nil {
		return err
	}
	return nil
}

func makeServingURLFromExternal(ctx context.Context, fileName, externalURL string) (string, error) {
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
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Criticalf(ctx, "%+v\n", err)
		return "", internalErrors.ErrSaveToDBFailed
	}
	defer conn.Close()
	if err = img.SaveURLToDB(ctx, conn); err != nil {
		log.Criticalf(ctx, "%+v\n", err)
		return "", err
	}
	return servingURL, nil
}

func makeServingURLFromGCS(ctx context.Context, gcsFileName string) (string, error) {
	img := &image.Image{FileName: gcsFileName}
	URL, err := img.CreateServingURL(ctx, bucketName)
	if err != nil {
		return "", err
	}
	return URL, nil
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
	errTemplate := "Missing environment variable %s"
	if productImagesDirectory == "" {
		panic(fmt.Sprintf(errTemplate, "PRODUCT_IMAGES_DIRECTORY"))
	}
	if bucketName == "" {
		var err error
		bucketName, err = file.DefaultBucketName(ctx)
		if err != nil {
			panic(err)
		}
	}
	if mysqlURL == "" {
		panic(fmt.Sprintf(errTemplate, "MYSQL_DATABASE_URL"))
	}
}
