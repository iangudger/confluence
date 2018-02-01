package confluence

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/net/websocket"
)

func execTemplate(tmpl *template.Template, w http.ResponseWriter, pc map[string]interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := tmpl.Execute(w, pc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

var mainTemplate = template.Must(template.New("main").Parse(`<!DOCTYPE html>
<html>
	<head>
		<title>Torrents</title>
	</head>
	<body>
		<h1>Torrents</h1>
		<form action="." method="post" enctype="multipart/form-data">
			<input name="file" type="file">
			<input type="submit" value="Add Torrent" name="submit">
		</form>
		<table border="1">
			<tr><th>Name</th><th>Link</th></tr>{{range $number, $torrent := $.torrents}}
			<tr><td>{{$torrent.Name}}</td><td><a href="/torrent/{{$torrent.Hash}}">info</a></td></tr>{{end}}
		</table>
	</body>
	<a href="/">Refresh</a>
</html>
`))

func (h *handler) mainHandler(w http.ResponseWriter, r *http.Request) {
	if mp, err := r.MultipartReader(); err == nil {
		for p, err := mp.NextPart(); err == nil; p, err = mp.NextPart() {
			if p.FormName() != "file" {
				continue
			}
			mi, err := metainfo.Load(p)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if _, err := h.client.AddTorrent(mi); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	type torrent struct {
		Name string
		Hash string
	}
	var torrents []torrent
	for _, t := range h.client.Torrents() {
		torrents = append(torrents, torrent{t.Name(), t.InfoHash().HexString()})
	}
	execTemplate(mainTemplate, w, map[string]interface{}{"torrents": torrents})
}

var torrentTemplate = template.Must(template.New("torrent").Parse(`<!DOCTYPE html>
<html>
	<head>
		<title>{{$.name}}</title>
	</head>
	<body>
		<h1>{{$.name}}</h1>
		<a href="{{$.path}}">Refresh</a>
		<a href="/">Back</a>
	</body>
</html>
`))

func (h *handler) torrentHandler(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 3 {
		http.Error(w, "Malformed torrent request", http.StatusNotFound)
		return
	}

	var ih metainfo.Hash
	if err := ih.FromHexString(parts[2]); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t, ok := h.client.Torrent(ih)
	if !ok {
		http.Error(w, "Torrent not found", http.StatusNotFound)
		return
	}

	execTemplate(torrentTemplate, w, map[string]interface{}{"name": t.Name(), "path": r.URL.Path})
}

func dataHandler(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	q := r.URL.Query()
	if len(q["path"]) == 0 {
		serveTorrent(w, r, t)
	} else {
		serveFile(w, r, t, q.Get("path"))
	}
}

func (h *handler) statusHandler(w http.ResponseWriter, r *http.Request) {
	h.client.WriteStatus(w)
}

func infoHandler(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	if nowait, err := strconv.ParseBool(r.URL.Query().Get("nowait")); err == nil && nowait {
		select {
		case <-t.GotInfo():
		default:
			http.Error(w, "info not ready", http.StatusAccepted)
			return
		}
	} else {
		// w.WriteHeader(http.StatusProcessing)
		select {
		case <-t.GotInfo():
		case <-r.Context().Done():
			return
		}
	}
	// w.WriteHeader(http.StatusOK)
	mi := t.Metainfo()
	w.Write(mi.InfoBytes)
}

func eventHandler(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	select {
	case <-t.GotInfo():
	case <-r.Context().Done():
		return
	}
	s := t.SubscribePieceStateChanges()
	defer s.Close()
	websocket.Server{
		Handler: func(c *websocket.Conn) {
			defer c.Close()
			readClosed := make(chan struct{})
			go func() {
				defer close(readClosed)
				c.Read(nil)
			}()
			for {
				select {
				case <-readClosed:
					eventHandlerWebsocketReadClosed.Add(1)
					return
				case <-r.Context().Done():
					eventHandlerContextDone.Add(1)
					return
				case _i := <-s.Values:
					i := _i.(torrent.PieceStateChange).Index
					if err := websocket.JSON.Send(c, Event{PieceChanged: &i}); err != nil {
						log.Printf("error writing json to websocket: %s", err)
						return
					}
				}
			}
		},
	}.ServeHTTP(w, r)
}

func fileStateHandler(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	path_ := r.URL.Query().Get("path")
	f := torrentFileByPath(t, path_)
	if f == nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(f.State())
}

func metainfoHandler(w http.ResponseWriter, r *http.Request, t *torrent.Torrent) {
	var mi metainfo.MetaInfo
	err := bencode.NewDecoder(r.Body).Decode(&mi)
	if err != nil {
		http.Error(w, fmt.Sprintf("error decoding body: %s", err), http.StatusBadRequest)
		return
	}
	t.AddTrackers(mi.UpvertedAnnounceList())
	t.SetInfoBytes(mi.InfoBytes)
	saveTorrentFile(t)
}
