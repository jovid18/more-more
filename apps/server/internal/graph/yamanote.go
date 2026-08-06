package graph

import "strings"

// 야마노테선 30역. 東京에서 시작해 外回り(시계 방향, 品川·渋谷·新宿 순) 순서다.
//
// 좌표는 아직 없다. 실제 위경도가 필요한 것은 가지 노선이 붙어 노선도가 겹칠
// 때이고, 그때 출처를 갖춘 데이터를 넣는다. M1에서 중요한 것은 위상(30역 순환)
// 뿐이라 클라이언트가 원형으로 배치해 그린다.
var yamanoteStations = []string{
	"東京|Tokyo",
	"有楽町|Yurakucho",
	"新橋|Shimbashi",
	"浜松町|Hamamatsucho",
	"田町|Tamachi",
	"高輪ゲートウェイ|Takanawa Gateway",
	"品川|Shinagawa",
	"大崎|Osaki",
	"五反田|Gotanda",
	"目黒|Meguro",
	"恵比寿|Ebisu",
	"渋谷|Shibuya",
	"原宿|Harajuku",
	"代々木|Yoyogi",
	"新宿|Shinjuku",
	"新大久保|Shin-Okubo",
	"高田馬場|Takadanobaba",
	"目白|Mejiro",
	"池袋|Ikebukuro",
	"大塚|Otsuka",
	"巣鴨|Sugamo",
	"駒込|Komagome",
	"田端|Tabata",
	"西日暮里|Nishi-Nippori",
	"日暮里|Nippori",
	"鶯谷|Uguisudani",
	"上野|Ueno",
	"御徒町|Okachimachi",
	"秋葉原|Akihabara",
	"神田|Kanda",
}

// Yamanote는 야마노테선 순환 그래프를 만든다.
//
// 30역이 하나의 사이클을 이룬다. 모든 역의 차수가 2이므로 갈림길이 없고,
// 플레이어의 선택은 "방향 전환" 하나뿐이다. 전략이 생기는 것은 가지 노선을
// 붙여 차수 3 이상인 역이 나타나는 시점부터다.
func Yamanote() *Graph {
	stations := make([]Station, len(yamanoteStations))
	for i, entry := range yamanoteStations {
		name, romaji := splitName(entry)
		stations[i] = Station{ID: StationID(i), Name: name, Romaji: romaji}
	}

	n := len(stations)
	edges := make([]Edge, n)
	for i := range n {
		// 마지막 역(神田)은 첫 역(東京)으로 이어져 고리를 닫는다.
		edges[i] = Edge{A: StationID(i), B: StationID((i + 1) % n)}
	}

	// 입력이 상수라 여기서 실패하면 이 파일의 버그다. 호출자에게 떠넘길 것이 없다.
	g, err := New(stations, edges)
	if err != nil {
		panic("graph: yamanote is malformed: " + err.Error())
	}
	return g
}

func splitName(entry string) (name, romaji string) {
	name, romaji, ok := strings.Cut(entry, "|")
	if !ok {
		romaji = name
	}
	return name, romaji
}
