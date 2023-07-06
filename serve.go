package main

import (
	// "bytes"
	"fmt"
	// "io/ioutil"
	// "net/http"
	"constvalue"
	"encoding/hex"
	"encoding/json"
	"github.com/bradfitz/iter"
	"os"
	"path/filepath"

	"github.com/anacrolix/log"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var Trackers = [][]string{
	{"udp://tracker.opentrackr.org:1337/announce"},
	{"udp://tracker.openbittorrent.com:6969/announce"},
	{"udp://tracker.moeking.me:6969/announce"},
	{"udp://p4p.arenabg.com:1337/announce"},
}

func fromMemory(byteData []byte) (*metainfo.MetaInfo, error) {
	info := metainfo.Info{}
	mi := metainfo.MetaInfo{}
	err := info.BuildFromMemory(byteData, "from memory")
	if err != nil {
		return nil, err
	}
	mi.SetDefaults()
	mi.InfoBytes = bencode.MustMarshal(info)
	mi.Announce = Trackers[0][0]
	mi.AnnounceList = Trackers

	return &mi, nil
}
func infoBytesToInfo(infoBytes []byte) (*metainfo.Info, error) {
	info := &metainfo.Info{}
	err := bencode.Unmarshal(infoBytes, info)
	if err != nil {
		return nil, err
	}
	return info, nil

}

func seed(mi *metainfo.MetaInfo, mbp *storage.MemoryBuf) (err error) {
	log.Printf("start seeding")
	clientConfig := torrent.NewDefaultClientConfig()
	clientConfig.Seed = true
	clientConfig.Debug = *debugFlag

	// 基于totalLength创建storage/client
	// clientConfig.DefaultStorage
	mbpp := &mbp
	storageImplCloser, err := storage.NewMemory(mbp.Length, mbpp)
	// storageImplCloser := storage.NewFile("./")
	if err != nil {
		return fmt.Errorf("NewMemory storage %w", err)
	}

	clientConfig.DefaultStorage = storageImplCloser
	client, err := torrent.NewClient(clientConfig)
	if err != nil {
		return fmt.Errorf("NewClient %w", err)
	}

	// 向client中添加torrent
	_, err = client.AddTorrent(mi)
	if err != nil {
		return fmt.Errorf("AddTorrent: %w", err)
	}
	return
}

func seed_another(byteData []byte, filePath string) error {
	log.Printf("serve")
	cfg := torrent.NewDefaultClientConfig()
	cfg.Seed = true
	cl, err := torrent.NewClient(cfg)
	if err != nil {
		return fmt.Errorf("new torrent client: %w", err)
	}
	defer cl.Close()

	info := metainfo.Info{}
	// err = info.BuildFromFilePath(filePath)
	err = info.BuildFromMemory(byteData, "from memory")
	log.Print(info.Pieces[:20])

	if err != nil {
		return fmt.Errorf("building info from memory %q: %w", constvalue.ModelName, err)
	}
	for _, fi := range info.Files {
		log.Printf("added %q", fi.Path)
	}

	mi := metainfo.MetaInfo{
		InfoBytes: bencode.MustMarshal(info),
	}

	pc, err := storage.NewDefaultPieceCompletionForDir(".")
	if err != nil {
		return fmt.Errorf("new piece completion: %w", err)
	}
	defer pc.Close()

	ih := mi.HashInfoBytes()
	to, _ := cl.AddTorrentOpt(torrent.AddTorrentOpts{
		InfoHash: ih,
		Storage: storage.NewFileOpts(storage.NewFileClientOpts{
			ClientBaseDir: filePath,
			FilePathMaker: func(opts storage.FilePathMakerOpts) string {
				return filepath.Join(opts.File.Path...)
			},
			TorrentDirMaker: nil,
			PieceCompletion: pc,
		}),
		InfoBytes: mi.InfoBytes,
	})
	defer to.Drop() // drop this torrent from the client torrent list after seeding

	err = to.MergeSpec(&torrent.TorrentSpec{
		Trackers: [][]string{
			{`wss://tracker.btorrent.xyz`},
			{`wss://tracker.openwebtorrent.com`},
			{"http://p4p.arenabg.com:1337/announce"},
			{"udp://tracker.opentrackr.org:1337/announce"},
			{"udp://tracker.openbittorrent.com:6969/announce"},
			{"udp://tracker.moeking.me:6969/announce"},
			{"udp://p4p.arenabg.com:1337/announce"},
		},
	})
	if err != nil {
		return fmt.Errorf("setting trackers: %w", err)
	}
	mi = to.Metainfo()
	// log.Print(mi.InfoBytes)
	pprintMetainfo(&mi, pprintMetainfoFlags{
		JustName:    false,
		PieceHashes: false,
		Files:       false})

	fmt.Printf("%v: %v\n", to, to.Metainfo().Magnet(&ih, &info))

	path := fmt.Sprintf("%s.torrent", info.BestName())
	mi.Write(os.Stdout)
	if err == nil {
		log.Printf("wrote %q", path)
	} else {
		log.Printf("error writing %q: %v", path, err)
	}

	select {}

}

type pprintMetainfoFlags struct {
	JustName    bool
	PieceHashes bool
	Files       bool
}

func pprintMetainfo(metainfo *metainfo.MetaInfo, flags pprintMetainfoFlags) error {
	info, err := metainfo.UnmarshalInfo()
	if err != nil {
		return fmt.Errorf("error unmarshalling info: %s", err)
	}
	if flags.JustName {
		fmt.Printf("%s\n", info.Name)
		return nil
	}
	d := map[string]interface{}{
		"Name":         info.Name,
		"Name.Utf8":    info.NameUtf8,
		"NumPieces":    info.NumPieces(),
		"PieceLength":  info.PieceLength,
		"InfoHash":     metainfo.HashInfoBytes().HexString(),
		"NumFiles":     len(info.UpvertedFiles()),
		"TotalLength":  info.TotalLength(),
		"Announce":     metainfo.Announce,
		"AnnounceList": metainfo.AnnounceList,
		"UrlList":      metainfo.UrlList,
	}
	if len(metainfo.Nodes) > 0 {
		d["Nodes"] = metainfo.Nodes
	}
	if flags.Files {
		d["Files"] = info.UpvertedFiles()
	}
	if flags.PieceHashes {
		d["PieceHashes"] = func() (ret []string) {
			for i := range iter.N(info.NumPieces()) {
				ret = append(ret, hex.EncodeToString(info.Pieces[i*20:(i+1)*20]))
			}
			return
		}()
	}
	b, _ := json.MarshalIndent(d, "", "  ")
	_, err = os.Stdout.Write(b)
	return err
}
