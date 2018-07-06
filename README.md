# Nearbuy Images

This service handles the generation of image serving URLs for the Nearbuy platform's products using the 
Google App Engine images service. This service was necessary because the images API is available on only
App Engine.

## Developing Nearbuy Images

Clone this repo to your local workstation and run `dep ensure` to pull the dependencies. Dependencies are
managed using `dep`.

Before deploying this app to App Engine, _delete the vendor directory_. This is necessary to appease the
`gcloud app deploy` command. A sample error message you may encounter deploying without deleting the _vendor_ directory is
> ERROR: (gcloud.app.deploy) Error Response: [9] Deployment contains files that cannot be compiled: Compile failed:
2018/07/06 11:25:28 go-app-builder: Failed parsing input: package "github.com/SprintHubNigeria/nearbuy-images/vendor/google.golang.org/appengine/image" cannot import internal package "google.golang.org/appengine/internal"

This is a known issue with the `gcloud` tool (See https://groups.google.com/forum/#!topic/google-appengine-go/Xooyiq3kFTI).
