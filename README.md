# a2s
- AWAのプレイリストを元に、Spotifyのプレイリストを生成する。
- AWAのプレイリストはパブリック状態である必要あり。

## インストール
```
go install
```

## 使い方
```
export TOKEN="your spotify token"
```

```
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
		a2s add https://mf.awa.fm/350pkxE https://open.spotify.com/playlist/2dpeGxTWfOVysBwuO5bvta
```

## Spotify のトークン
以下のスコープが必要。

- user-read-private 
- user-read-email
- playlist-modify-private
- playlist-modify-public
