package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"go.uber.org/multierr"
)

var (
	isDebug = os.Getenv("DEBUG") != ""
	cli     *client
)

const trackCnt = 8

type playlist struct {
	name        string
	description string
	tracks      []*track
}

type track struct {
	name   string
	artist string
}

func debug(format string, v ...interface{}) {
	if isDebug {
		fmt.Printf(format+"\n", v...)
	}
}

// scrape scrapes awa playlist page.
func scrape(url string) (*playlist, error) {
	debug("*** scrape")
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status code %d is not 200", resp.StatusCode)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	var p playlist
	p.name = doc.Find("._38UsOh4Z6h0g6W85obDl_M.-fw-b").First().Text()
	if p.name == "" {
		return nil, fmt.Errorf("could not find playlist name")
	}
	debug("name: %s", p.name)

	// なんか "…もっと見る" がつくので外してる。
	// もっといいやり方があるかも？
	p.description = strings.TrimRight(
		doc.Find(".cSux9HGnsrA6Wg6YcZJpP._2VQVMPZjwSZ7gutPRRfXQh._1nQ5k5yMiVg8rurXPOKTTJ").First().Text(),
		"…もっと見る",
	)
	if p.description == "" {
		return nil, fmt.Errorf("could not find description")
	}
	debug("description: %s", p.description)

	names := make([]string, 0, trackCnt)
	artists := make([]string, 0, trackCnt)
	doc.Find(".c1tzH5-SsFpW2sQBsrLLg._2Fb6XA6X_L7NVOLEUR3qN4").Each(func(i int, s *goquery.Selection) {
		if i%2 == 0 {
			names = append(names, s.Text())
			if s.Text() == "" {
				err = multierr.Append(err, fmt.Errorf("could not find track name, i=%d", i))
			}
			debug("%2d: name=%s", i, s.Text())
		} else {
			artists = append(artists, s.Text())
			if s.Text() == "" {
				err = multierr.Append(err, fmt.Errorf("could not find artist, i=%d", i))
			}
			debug("%2d: artist=%s", i, s.Text())
		}
	})
	if err != nil {
		return nil, err
	}
	p.tracks = make([]*track, trackCnt)
	for i := 0; i < trackCnt; i++ {
		p.tracks[i] = &track{
			name:   names[i],
			artist: artists[i],
		}
	}

	return &p, nil
}

// create creates playlist on spotify.
func create(p *playlist) (string, error) {
	debug("*** create")
	// ユーザーIDの取得
	endpoint := "https://api.spotify.com/v1/me"
	resp, err := cli.get(endpoint)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result := make(map[string]interface{})
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	debug("userProfile.get result: %v", result)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("could not get user profile: url=%s, result=%v", endpoint, result)
	}
	user := result["id"].(string)
	debug("user: %s", user)

	endpoint = fmt.Sprintf("https://api.spotify.com/v1/users/%s/playlists", user)
	req := struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}{
		Name:        p.name,
		Description: strings.ReplaceAll(p.description, "\n", ""),
	}
	resp, err = cli.post(endpoint, &req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	result = make(map[string]interface{})
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	debug("playlist.create result: %v", result)
	if (resp.StatusCode != http.StatusOK) && (resp.StatusCode != http.StatusCreated) {
		return "", fmt.Errorf("could not create playlist: url=%s, req=%+v, result=%v", endpoint, req, result)
	}
	playlistLink := result["external_urls"].(map[string]interface{})["spotify"].(string)
	debug("playlist link: %s", playlistLink)
	playlistID := result["id"].(string)
	debug("playlistID: %s", playlistID)

	if err := add(playlistID, p.tracks); err != nil {
		return "", err
	}
	return playlistLink, nil
}

// add appends playlist on spotify.
func add(playlistID string, tracks []*track) error {
	debug("*** add")
	uris := make([]string, 0, trackCnt)
	for _, track := range tracks {
		debug("****** %s / %s", track.name, track.artist)
		endpoint := fmt.Sprintf(
			"https://api.spotify.com/v1/search?q=%s&type=track&limit=1",
			url.QueryEscape(fmt.Sprintf("%s %s",
				// HACK:
				// 1. "Cymbals/古市 コータロー/内田 晴元/西野 千菜美米山 美弥子" というアーティスト名だと検索できなかったが
				//    "/" を " " に置換すれば検索できるようになる。
				// 2. "Tomggg feat. Raychel Jay" というアーティスト名だと検索できなかったが
				//    "feat." を取り除けば検索できるようになる。
				track.name, strings.ReplaceAll(strings.ReplaceAll(track.artist, "/", " "), "feat.", ""))),
		)
		debug("url: %s", endpoint)
		resp, err := cli.get(endpoint)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		result := make(map[string]interface{})
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return err
		}
		debug("search result: %v", result)
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("could not search: url=%s, result=%v", endpoint, result)
		}

		items := result["tracks"].(map[string]interface{})["items"].([]interface{})
		if len(items) != 1 {
			fmt.Printf("[!] Could not find \"%s / %s\"\n", track.name, track.artist)
			continue
		}

		uri := items[0].(map[string]interface{})["uri"].(string)
		uris = append(uris, uri)
		debug("uri: %s", uri)
		link := items[0].(map[string]interface{})["external_urls"].(map[string]interface{})["spotify"].(string)
		fmt.Printf("%s / %s -> %s\n", track.name, track.artist, link)
		debug("link: %s", link)
	}
	req := struct {
		URIS []string `json:"uris"`
	}{
		URIS: uris,
	}
	endpoint := fmt.Sprintf("https://api.spotify.com/v1/playlists/%s/tracks", playlistID)
	resp, err := cli.post(endpoint, &req)
	if err != nil {
		return err
	}
	result := make(map[string]interface{})
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	debug("playlist.addItems result: %v", result)
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("could not add items: url=%s, req=%+v, result=%v", endpoint, req, result)
	}
	return nil
}

type client struct {
	c     *http.Client
	token string
}

func (c *client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
}

func (c *client) get(url string) (resp *http.Response, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	return c.c.Do(req)
}

func (c *client) post(url string, req interface{}) (*http.Response, error) {
	reqJson, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequest("POST", url, bytes.NewBuffer(reqJson))
	c.setAuth(r)
	r.Header.Set("Content-Type", "application/json")
	if err != nil {
		return nil, err
	}
	return c.c.Do(r)
}

func parsePlaylistID(url string) string {
	ret := strings.Split(url, "/")
	return ret[len(ret)-1]
}

func main() {
	t := os.Getenv("TOKEN")
	if t == "" {
		fmt.Println("the environment variable \"TOKEN\" must be set to spotify token.")
		os.Exit(1)
	}
	cli = &client{
		c:     http.DefaultClient,
		token: t,
	}

	const (
		createCmdStr = "create"
		addCmdStr    = "add"
	)

	var (
		createCmd = flag.NewFlagSet(createCmdStr, flag.ExitOnError)
		name      = createCmd.String("name", "", "Playlist name.")
		desc      = createCmd.String("desc", "", "Playlist description.")
	)

	flag.Usage = func() {
		txt := `
Usage:
	a2s [command]

Command:
	create AWAのプレイリストを元にSpotifyのプレイリストを作成する。
	add    既存のSpotifyのプレイリストにAWAのプレイリストのトラックを追加する。

Create command:
	Usage:
		a2s create [awa playlist url] [options]

	Options:
		-name 作成するプレイリストの名前。デフォルトはAWAのプレイリストの名前。
		-desc 作成するプレイリストの説明文。デフォルトはAWAのプレイリストの説明文。

	Example:
		a2s create https://mf.awa.fm/2RDS2S8 -name="今日の1曲" -desc="素敵な音楽がいっぱいあって幸せです" 

Add command:
	Usage:
		a2s add [awa playlist url] [spotify playlist url]
	
	Example:
		a2s add https://mf.awa.fm/350pkxE https://open.spotify.com/playlist/2dpeGxTWfOVysBwuO5bvta`

		fmt.Println(txt)
	}

	if len(os.Args) < 2 {
		flag.Usage()
		return
	}

	switch os.Args[1] {
	case createCmdStr:
		if err := createCmd.Parse(os.Args[3:]); err != nil {
			fmt.Fprintf(os.Stderr, "Could not parse create commands: %v\n", err)
			return
		}
		if len(os.Args) < 3 {
			fmt.Println("AWA playlist url must be set.")
			flag.Usage()
			return
		}

		p, err := scrape(os.Args[2])
		if err != nil {
			fmt.Printf("Could not scrape: %v\n", err)
			return
		}
		if *name != "" {
			p.name = *name
		}
		if *desc != "" {
			p.description = *desc
		}
		url, err := create(p)
		if err != nil {
			fmt.Printf("Could not create: %v\n", err)
			return
		}
		fmt.Println("playlist url:", url)

	case addCmdStr:
		if len(os.Args) < 4 {
			fmt.Println("AWA and Spotify playlist url must be set.")
			flag.Usage()
			return
		}
		p, err := scrape(os.Args[2])
		if err != nil {
			fmt.Printf("Could not scrape: %v\n", err)
			return
		}
		if err := add(parsePlaylistID(os.Args[3]), p.tracks); err != nil {
			fmt.Printf("Could not add: %v\n", err)
			return
		}

	default:
		fmt.Println("Unknown command.")
		flag.Usage()
		return
	}
}
