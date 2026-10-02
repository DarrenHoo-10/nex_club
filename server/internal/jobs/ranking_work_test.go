package jobs

import (
	"context"
	"strings"
	"testing"

	"github.com/riverqueue/river"

	"github.com/darrenhoo/nex_club/server/internal/ranking"
)

func TestRankingWorkerBody(t *testing.T) {
	if err := (RankingWorker{}).Work(context.Background(), nil); err == nil {
		t.Fatal("missing job")
	}
	ranking.Bind(&ranking.Service{})
	t.Cleanup(func() { ranking.Bind(nil) })
	err := (RankingWorker{}).Work(context.Background(), &river.Job[RankingArgs]{
		Args: RankingArgs{Section: "nope", Reason: "test"},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid ranking section") {
		t.Fatal(err)
	}
}
