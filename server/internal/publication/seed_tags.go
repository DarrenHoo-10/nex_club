package publication

// seedTagSlugs maps every Chinese label in src/data to a capability slug.
// Names missing from this table fail the import. Nothing is transliterated.
var seedTagSlugs = map[string]string{
	"Agent":  "agent",
	"Claude": "claude",
	"RAG":    "rag",
	"入门":     "beginner",
	"写作":     "writing",
	"创作":     "creation",
	"图像":     "image",
	"多模态":    "multimodal",
	"大模型":    "llm",
	"学习":     "learning",
	"安全":     "security",
	"对话":     "chat",
	"工作流":    "workflow",
	"工具":     "tools",
	"平台":     "platform",
	"开源":     "open-source",
	"推理":     "inference",
	"提示词":    "prompts",
	"搜索":     "search",
	"效率":     "productivity",
	"文档":     "documents",
	"本地部署":   "local",
	"框架":     "framework",
	"模型":     "models",
	"浏览器":    "browser",
	"演示":     "slides",
	"界面":     "ui",
	"社区":     "community",
	"编程":     "coding",
	"网站":     "website",
	"自动化":    "automation",
	"自托管":    "self-host",
	"订阅":     "subscription",
	"设计":     "design",
	"调研":     "research",
	"运维":     "operations",
	"进阶":     "advanced",
	"配音":     "voice",
	"音频":     "audio",
}

func seedTagSlug(name string) (string, bool) {
	slug, ok := seedTagSlugs[name]
	return slug, ok
}
