package present

import (
	"encoding/json"
	"testing"

	"github.com/darrenhoo/nex_club/server/internal/catalog"
)

func TestCardFor(t *testing.T) {
	tool, err := CardFor(catalog.KindTool, json.RawMessage(`{"website_url":"https://www.Perplexity.ai/search","pricing":"freemium"}`))
	if err != nil {
		t.Fatal(err)
	}
	if tool.Subtitle != "perplexity.ai" || tool.Meta != "免费 + 付费" || tool.CTA != "访问" || tool.Href == nil {
		t.Fatalf("%+v", tool)
	}
	tutorial, err := CardFor(catalog.KindTutorial, json.RawMessage(`{"level":"beginner","minutes":5,"steps":["a"," b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.Subtitle != "5 分钟 · 2 步" || tutorial.Meta != "入门" || tutorial.Href != nil || tutorial.CTA != "查看教程" {
		t.Fatalf("%+v", tutorial)
	}
	repo, err := CardFor(catalog.KindRepo, json.RawMessage(`{"full_name":"ggerganov/llama.cpp","language":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if repo.Subtitle != "ggerganov" || repo.Meta != "未知语言" || repo.Href == nil || *repo.Href != "https://github.com/ggerganov/llama.cpp" {
		t.Fatalf("%+v", repo)
	}
}

func TestSearchTextFolds(t *testing.T) {
	got := SearchText(TextInput{
		Title:    "  ＡＩ  Claude ",
		Aliases:  []string{"ChatGPT"},
		TagNames: []string{"对话"},
		Summary:  "写  代码",
		Steps:    []string{"本地部署"},
	})
	want := "ai claude chatgpt 对话 写 代码 本地部署"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if Normalize("配音") != "配音" {
		t.Fatal(Normalize("配音"))
	}
}
