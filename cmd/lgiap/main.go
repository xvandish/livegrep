package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/livegrep/livegrep/server/api"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 2 && os.Args[1] == "configure":
		err = configure(
			context.Background(),
			os.Getenv("CLIENT_ID"),
			os.Getenv("CLIENT_SECRET"),
			os.Getenv("IAP_CLIENT_ID"),
		)
	case len(os.Args) == 3 && os.Args[1] == "composite":
		err = compositeQuery(context.Background(), os.Args[2])
	default:
		err = query(context.Background(), os.Args[1:])
	}
	if err != nil {
		log.Fatal(err)
	}
}

var knownOperators = map[string]struct{}{"": {}, "include": {}, "exclude_by_file": {}, "exclude_by_repo": {}}

func compositeQuery(ctx context.Context, path string) error {
	type (
		queryExpression struct {
			Query    string `json:"query"`
			Operator string `json:"operator"`
		}
		exclusionKey struct {
			Tree    string `json:"tree"`
			Version string `json:"version"`
			Path    string `json:"path"`
		}
	)

	var composition struct {
		Queries      []*queryExpression `json:"queries"`
		ExcludeFiles []exclusionKey     `json:"exclude_files"`
		VersionAware bool               `json:"version_aware"`
		MaxMatches   int                `json:"max_matches"`
	}

	{
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() {
			_ = f.Close()
		}()
		if err = json.NewDecoder(f).Decode(&composition); err != nil {
			return err
		}
		for _, q := range composition.Queries {
			if _, ok := knownOperators[q.Operator]; !ok {
				return fmt.Errorf("unknown operator: %v", q.Operator)
			}
			if composition.MaxMatches == 0 {
				continue
			}
			if strings.Contains(q.Query, "max_matches:") {
				return fmt.Errorf("duplicate max_matches field")
			}
			q.Query += fmt.Sprintf(" max_matches:%d", composition.MaxMatches)
		}
	}

	ctx, cncl := context.WithCancel(ctx)
	defer cncl()

	token, err := getIDToken(ctx)
	if err != nil {
		return err
	}

	tmpDir, err := os.MkdirTemp("", "codesearch.****")
	if err != nil {
		return err
	}
	defer func() {
		if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
			log.Printf("Removing the tmpdir: %v", rmErr)
		}
	}()

	type result struct {
		*os.File
		*queryExpression
		err error
	}

	resultCh := make(chan *result)
	for _, q := range composition.Queries {
		go func(q *queryExpression) {
			r := result{queryExpression: q}
			defer func() {
				if r.err != nil && r.File != nil {
					_ = r.File.Close()
					r.File = nil
				}
				select {
				case <-ctx.Done():
				case resultCh <- &r:
				}
			}()

			r.File, r.err = os.CreateTemp(tmpDir, "")
			if r.err != nil {
				return
			}
			if r.err = doQuery(ctx, token, q.Query, r); r.err != nil {
				return
			}
			_, r.err = r.Seek(0, io.SeekStart)
		}(q)
	}

	var results []*result
	for len(results) != len(composition.Queries) {
		select {
		case r := <-resultCh:
			if r.err != nil {
				cncl()
				return r.err
			}
			results = append(results, r)
		case <-ctx.Done():
			return nil
		}
	}

	makeExcludedFileKey := func(res *api.Result) exclusionKey {
		k := exclusionKey{Tree: res.Tree, Version: res.Version, Path: res.Path}
		if !composition.VersionAware {
			k.Version = ""
		}
		return k
	}
	makeExcludedRepoKey := func(res *api.Result) exclusionKey {
		k := makeExcludedFileKey(res)
		k.Path = ""
		return k
	}

	var (
		allFound      = make([]*api.Result, 0) // init to avoid null empty list
		excludedFiles = make(map[exclusionKey]struct{}, len(composition.ExcludeFiles))
		excludedRepos = make(map[exclusionKey]struct{})
	)
	for _, e := range composition.ExcludeFiles {
		excludedFiles[e] = struct{}{}
	}

	// all queries have succeeded
	for _, r := range results {
		var reply api.ReplySearch
		if err := json.NewDecoder(r.File).Decode(&reply); err != nil {
			return err
		}
		switch r.Operator {
		case "", "include":
			allFound = append(allFound, reply.Results...)
		case "exclude_by_file":
			for _, res := range reply.Results {
				excludedFiles[makeExcludedFileKey(res)] = struct{}{}
			}
		case "exclude_by_repo":
			for _, res := range reply.Results {
				excludedRepos[makeExcludedRepoKey(res)] = struct{}{}
			}
		default:
			panic(fmt.Sprintf("unknown operator: %v", r.Operator)) // guarded above
		}
	}

	type dedupeKey struct {
		Tree, Version, Path string
		LineNumber          int
	}

	var (
		makeDedupeKey = func(res *api.Result) dedupeKey {
			k := dedupeKey{Tree: res.Tree, Version: res.Version, Path: res.Path, LineNumber: res.LineNumber}
			if !composition.VersionAware {
				k.Version = ""
			}
			return k
		}
		seen = make(map[dedupeKey]struct{})
	)
	for _, found := range allFound {
		if _, ok := excludedFiles[makeExcludedFileKey(found)]; ok {
			continue
		}
		if _, ok := excludedRepos[makeExcludedRepoKey(found)]; ok {
			continue
		}
		if _, ok := seen[makeDedupeKey(found)]; ok {
			continue
		}

		allFound[len(seen)] = found
		seen[makeDedupeKey(found)] = struct{}{}
	}

	return formatResults(allFound[:len(seen)], composition.VersionAware, os.Stdout)
}

func formatResults(results []*api.Result, versionAware bool, w io.Writer) error {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Tree < results[j].Tree {
			return true
		}
		if results[j].Tree < results[i].Tree {
			return false
		}
		if results[i].Path < results[j].Path {
			return true
		}
		if results[j].Path < results[i].Path {
			return false
		}
		if versionAware {
			if results[i].Version < results[j].Version {
				return true
			}
			if results[j].Version < results[i].Version {
				return false
			}
		}
		return results[i].LineNumber < results[j].LineNumber
	})

	return json.NewEncoder(w).Encode(results)
}

func doQuery(ctx context.Context, idToken, query string, w io.Writer) error {
	u := (&url.URL{
		Scheme:   "https",
		Host:     "codesearch.nyt.net",
		Path:     "/api/v1/search",
		RawQuery: url.Values{"q": []string{query}}.Encode(),
	}).String()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", idToken))

	resp, err := require2xx(http.DefaultClient.Do(req))
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	_, err = io.Copy(w, resp.Body)
	return err
}

func query(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("at least one argument must be provided")
	}

	idToken, err := getIDToken(ctx)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err = doQuery(ctx, idToken, strings.Join(args, " "), &buf); err != nil {
		return err
	}

	var reply api.ReplySearch
	if err = json.NewDecoder(&buf).Decode(&reply); err != nil {
		return err
	}

	return formatResults(reply.Results, false, os.Stdout)
}

type iapCreds struct {
	ClientID     string
	ClientSecret string
	RefreshToken string
	Audience     string
}

func getIDToken(ctx context.Context) (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	f, err := os.Open(path.Join(cacheDir, "codesearch", "iap-creds.json"))
	if err != nil {
		return "", err
	}
	defer func() {
		_ = f.Close()
	}()

	r := json.NewDecoder(f)

	var creds iapCreds
	if err = r.Decode(&creds); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(
		ctx,
		"POST",
		"https://oauth2.googleapis.com/token",
		strings.NewReader((url.Values{
			"client_id":     []string{creds.ClientID},
			"client_secret": []string{creds.ClientSecret},
			"refresh_token": []string{creds.RefreshToken},
			"grant_type":    []string{"refresh_token"},
			"audience":      []string{creds.Audience},
		}).Encode()),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := require2xx(http.DefaultClient.Do(req))
	if err != nil {
		return "", err
	}
	var idTokenResp struct {
		IDToken string `json:"id_token"`
	}
	err = unmarshalJSONResponse(resp, &idTokenResp)
	return idTokenResp.IDToken, err
}

func configure(ctx context.Context, clientID, clientSecret, iapClientID string) error {
	if clientID == "" || clientSecret == "" || iapClientID == "" {
		return errors.New("all of CLIENT_ID, CLIENT_SECRET and IAP_CLIENT_ID are required for configure")
	}

	refreshToken, err := getRefreshToken(ctx, clientID, clientSecret)
	if err != nil {
		return fmt.Errorf("get auth code: %w", err)
	}

	creds := iapCreds{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RefreshToken: refreshToken,
		Audience:     iapClientID,
	}

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}

	cacheFile := path.Join(cacheDir, "codesearch", "iap-creds.json")

	if err = os.Mkdir(path.Dir(cacheFile), 0700); err != nil && !os.IsExist(err) {
		return err
	}

	f, err := os.OpenFile(cacheFile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	w := json.NewEncoder(f)
	w.SetIndent("", "  ")
	return w.Encode(creds)
}

type crappyHandler struct {
	Codes chan<- string
}

func (c *crappyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case c.Codes <- r.URL.Query().Get("code"):
		_, _ = fmt.Fprintln(w, "Success! Please return to your command line -- you may close this tab.")
	case <-r.Context().Done():
		log.Println("Request ended waiting to send output")
	}
}

func getRefreshToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	ctx, cncl := context.WithTimeout(ctx, time.Second*30)
	defer cncl()

	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return "", fmt.Errorf("unable to open listener: %w", err)
	}

	var (
		codes   = make(chan string)
		c       = crappyHandler{codes}
		srvr    = http.Server{Handler: &c}
		srvrURL = url.URL{Scheme: "http", Host: listener.Addr().String()}
	)
	defer func() {
		ctx, cncl := context.WithTimeout(ctx, time.Second*1)
		defer cncl()
		_ = srvr.Shutdown(ctx)
	}()
	go func() {
		_ = srvr.Serve(listener)
	}()

	authURL := url.URL{
		Scheme: "https",
		Host:   "accounts.google.com",
		Path:   "/o/oauth2/v2/auth",
		RawQuery: url.Values{
			"client_id":     []string{clientID},
			"response_type": []string{"code"},
			"scope":         []string{"openid email"},
			"access_type":   []string{"offline"},
			"redirect_uri":  []string{srvrURL.String()},
		}.Encode(),
	}

	log.Printf("opening %q in a browser", authURL.String())
	openCmd := exec.CommandContext(context.Background(), "open", authURL.String())
	if err = openCmd.Start(); err != nil {
		return "", fmt.Errorf("unable to open browser: %w", err)
	}

	var code string
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case code = <-codes:
	}

	resp, err := require2xx(http.PostForm(
		(&url.URL{
			Scheme: "https",
			Host:   "oauth2.googleapis.com",
			Path:   "/token",
		}).String(),
		url.Values{
			"client_id":     []string{clientID},
			"client_secret": []string{clientSecret},
			"code":          []string{code},
			"grant_type":    []string{"authorization_code"},
			"redirect_uri":  []string{srvrURL.String()},
		},
	))
	if err != nil {
		return "", err
	}
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	err = unmarshalJSONResponse(resp, &body)
	return body.RefreshToken, err
}

func unmarshalJSONResponse(resp *http.Response, val interface{}) error {
	defer func() {
		_ = resp.Body.Close()
	}()
	return json.NewDecoder(resp.Body).Decode(&val)
}

func require2xx(resp *http.Response, err error) (*http.Response, error) {
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 == 2 {
		return resp, nil
	}
	errMsgB, _ := io.ReadAll(resp.Body)
	errMsg := string(errMsgB)
	_ = resp.Body.Close()

	if len(errMsg) == 0 {
		errMsg = http.StatusText(resp.StatusCode)
	}
	return nil, fmt.Errorf("unexpected status code: %v (%v)", resp.StatusCode, errMsg)
}
