package confluence

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/anacrolix/missinggo/refclose"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

const infohashQueryKey = "ih"

func infohashFromQueryOrServeError(w http.ResponseWriter, q url.Values) (ih metainfo.Hash, ok bool) {
	if err := ih.FromHexString(q.Get(infohashQueryKey)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ok = true
	return
}

// Handles ref counting, close grace, and various torrent client wrapping
// work.
func getTorrentHandle(r *http.Request, ih metainfo.Hash, client *torrent.Client, closeGrace time.Duration) *torrent.Torrent {
	var ref *refclose.Ref
	if closeGrace >= 0 {
		ref = torrentRefs.NewRef(ih)
	}
	t, new := client.AddTorrentInfoHash(ih)
	if closeGrace >= 0 {
		ref.SetCloser(t.Drop)
		go func() {
			defer time.AfterFunc(closeGrace, ref.Release)
			<-r.Context().Done()
		}()
	}
	if new {
		mi := cachedMetaInfo(ih)
		if mi != nil {
			t.AddTrackers(mi.UpvertedAnnounceList())
			t.SetInfoBytes(mi.InfoBytes)
		}
		go saveTorrentWhenGotInfo(t)
	}
	return t
}

func (h *handler) withTorrent(f func(http.ResponseWriter, *http.Request, *torrent.Torrent)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ih, ok := infohashFromQueryOrServeError(w, r.URL.Query())
		if !ok {
			return
		}
		f(w, r, getTorrentHandle(r, ih, h.client, h.closeGrace))
	}
}

func saveTorrentWhenGotInfo(t *torrent.Torrent) {
	select {
	case <-t.Closed():
	case <-t.GotInfo():
	}
	err := saveTorrentFile(t)
	if err != nil {
		log.Printf("error saving torrent file: %s", err)
	}
}

func cachedMetaInfo(infoHash metainfo.Hash) *metainfo.MetaInfo {
	p := fmt.Sprintf("torrents/%s.torrent", infoHash.HexString())
	mi, err := metainfo.LoadFromFile(p)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		log.Printf("error loading metainfo file %q: %s", p, err)
	}
	return mi
}
