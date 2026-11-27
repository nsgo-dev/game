package achievements_test

import (
	"fmt"

	"github.com/nsgo-dev/game/pkg/achievements"
	"github.com/nsgo-dev/nsgo/core"
)

const exampleAchievements = `version: 1
achievements:
  - id: first-match
    title: First match
    platforms:
      gameCenter: com.example.first_match
      playGames: CgkIxxxxEAIQAQ
  - id: gem-collector
    title: Gem collector
    steps: 4
    platforms:
      gameCenter: com.example.gems
      playGames: CgkIxxxxEAIQAg
`

// memoryStore is a Store in a map; a game passes core.Storage instead.
type memoryStore map[string]string

func (m memoryStore) GetItem(key string) (string, bool) { v, ok := m[key]; return v, ok }
func (m memoryStore) SetItem(key, value string) error   { m[key] = value; return nil }

// A Tracker records progress toward a Set's achievements, calls OnUnlock once when one completes, and reports progress to the connected service under that service's own ID.
func ExampleTracker() {
	set, err := achievements.Parse([]byte(exampleAchievements))
	if err != nil {
		fmt.Println(err)
		return
	}
	tr, err := achievements.NewTracker(set)
	if err != nil {
		fmt.Println(err)
		return
	}
	tr.OnUnlock(func(d achievements.Definition) { fmt.Println("unlocked:", d.Title) })

	// Progress made before sign-in is kept and reported once a service connects.
	_ = tr.Add("gem-collector", 3)
	fmt.Println(tr.Pending(achievements.GameCenter))
	_ = tr.Connect(achievements.GameCenter, achievements.ReporterFunc(func(id string, percent float64, done func(error)) {
		fmt.Printf("report %s %g%%\n", id, percent)
		done(nil)
	}))
	_ = tr.Add("gem-collector", 5)
	_ = tr.Unlock("first-match")
	fmt.Println(tr.Percent("gem-collector"), len(tr.Pending(achievements.GameCenter)))
	// Output:
	// [gem-collector]
	// report com.example.gems 75%
	// unlocked: Gem collector
	// report com.example.gems 100%
	// unlocked: First match
	// report com.example.first_match 100%
	// 100 0
}

// Persist keeps the Tracker's progress in a Store, such as core.Storage, so a new session picks up where the last left off.
func ExampleTracker_Persist() {
	set, _ := achievements.Parse([]byte(exampleAchievements))
	store := memoryStore{}

	first, _ := achievements.NewTracker(set)
	_ = first.Persist(store, "achievements")
	_ = first.Add("gem-collector", 2)

	second, _ := achievements.NewTracker(set)
	err := second.Persist(store, "achievements")
	fmt.Println(err, second.Steps("gem-collector"), second.Percent("gem-collector"))
	// Output:
	// <nil> 2 50
}

// Parse refuses unknown fields; Encode writes the file back byte for byte.
func ExampleParse() {
	set, err := achievements.Parse([]byte(exampleAchievements))
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, d := range set.Achievements {
		fmt.Println(d.ID, d.TotalSteps(), d.Platforms.GameCenter)
	}
	out, _ := achievements.Encode(set)
	fmt.Println(string(out) == exampleAchievements)
	_, err = achievements.Parse([]byte(exampleAchievements + "points: 10\n"))
	fmt.Println(err != nil)
	// Output:
	// first-match 1 com.example.first_match
	// gem-collector 4 com.example.gems
	// true
	// true
}

// ServiceFor names the game service a platform reports achievements to.
func ExampleServiceFor() {
	fmt.Printf("%q %q %q\n", achievements.ServiceFor(core.OSIOS), achievements.ServiceFor(core.OSAndroid), achievements.ServiceFor(core.OSLinux))
	// Output:
	// "gameCenter" "playGames" ""
}
