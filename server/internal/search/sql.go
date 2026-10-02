package search

func normExpr(expr string) string {
	return "regexp_replace(btrim(lower(normalize(coalesce(" + expr + ", ''), NFKC))), '[[:space:]]+', ' ', 'g')"
}

func matchTierExpr() string {
	title := normExpr("p.title")
	summary := normExpr("p.summary")
	body := normExpr("p.body_markdown")
	alias := normExpr("alias_value")
	tagName := normExpr("t.name")
	tagAlias := normExpr("ta.alias")
	step := normExpr("step_value")
	return `CASE
		WHEN NOT CAST(@has_query AS boolean) THEN NULL
		WHEN ` + title + ` = CAST(@q AS text) THEN 1
		WHEN strpos(` + title + `, CAST(@q AS text)) > 0 OR EXISTS (
			SELECT 1 FROM unnest(p.aliases) AS alias_value
			WHERE strpos(` + alias + `, CAST(@q AS text)) > 0
		) THEN 2
		WHEN EXISTS (
			SELECT 1
			FROM resource_tags rt
			JOIN tags t ON t.id = rt.tag_id
			LEFT JOIN tag_aliases ta ON ta.tag_id = t.id
			WHERE rt.resource_id = r.id
			  AND (
				strpos(` + tagName + `, CAST(@q AS text)) > 0
				OR (ta.normalized_alias IS NOT NULL AND strpos(ta.normalized_alias, CAST(@q AS text)) > 0)
				OR (ta.alias IS NOT NULL AND strpos(` + tagAlias + `, CAST(@q AS text)) > 0)
			  )
		) THEN 3
		WHEN strpos(` + summary + `, CAST(@q AS text)) > 0 THEN 4
		WHEN strpos(` + body + `, CAST(@q AS text)) > 0 OR EXISTS (
			SELECT 1
			FROM jsonb_array_elements_text(
				CASE
					WHEN jsonb_typeof(p.details->'steps') = 'array' THEN p.details->'steps'
					ELSE '[]'::jsonb
				END
			) AS step_value
			WHERE strpos(` + step + `, CAST(@q AS text)) > 0
		) THEN 5
		ELSE NULL
	END`
}

func tagJSON() string {
	return `COALESCE((
		SELECT jsonb_agg(jsonb_build_object(
			'id', t.id::text,
			'name', t.name,
			'slug', t.slug,
			'dimension', t.dimension
		) ORDER BY t.dimension, t.slug)
		FROM resource_tags rt
		JOIN tags t ON t.id = rt.tag_id
		WHERE rt.resource_id = r.id
	), '[]'::jsonb)`
}

func publicWhere() string {
	return `r.kind = CAST(@kind AS text)
		AND r.status = 'published'
		AND (CAST(@include_demo AS boolean) OR NOT r.is_demo)`
}

func textWhere() string {
	return `(
		NOT CAST(@has_query AS boolean)
		OR p.search_text ILIKE '%' || CAST(@like AS text) || '%' ESCAPE '\'
	)`
}

func tagWhere() string {
	return `(
		cardinality(CAST(@tag_slugs AS text[])) = 0
		OR (
			SELECT count(DISTINCT t.slug)::int
			FROM resource_tags rt
			JOIN tags t ON t.id = rt.tag_id
			WHERE rt.resource_id = r.id
			  AND t.slug = ANY(CAST(@tag_slugs AS text[]))
		) = cardinality(CAST(@tag_slugs AS text[]))
	)`
}

func listSQL() string {
	return `
		SELECT
			listed.id,
			listed.kind,
			listed.slug,
			listed.title,
			listed.summary,
			listed.cover_urls,
			listed.quality_score,
			listed.first_published_at,
			listed.content_updated_at,
			listed.details,
			listed.primary_category_id,
			listed.primary_category_name,
			listed.primary_category_slug,
			listed.primary_category_dimension,
			listed.tags,
			listed.match_tier,
			listed.recommendation_score,
			listed.heat_position,
			listed.recommendation_position
		FROM (
			SELECT
				r.id,
				r.kind,
				r.slug,
				p.title,
				p.summary,
				p.cover_urls,
				p.quality_score,
				r.first_published_at,
				p.content_updated_at,
				p.details,
				pc.id AS primary_category_id,
				pc.name AS primary_category_name,
				pc.slug AS primary_category_slug,
				pc.dimension AS primary_category_dimension,
				` + tagJSON() + ` AS tags,
				` + matchTierExpr() + ` AS match_tier,
				COALESCE(e.recommendation_score, 0)::text AS recommendation_score,
				e.heat_position,
				e.recommendation_position
			FROM resources r
			JOIN resource_publications p ON p.resource_id = r.id
			LEFT JOIN tags pc ON pc.id = p.primary_category_id
			LEFT JOIN ranking_entries e
				ON CAST(@has_run AS boolean)
				AND e.run_id = CAST(@run_id AS uuid)
				AND e.resource_id = r.id
				AND e.kind = r.kind
			WHERE ` + publicWhere() + `
			  AND ` + tagWhere() + `
			  AND ` + textWhere() + `
		) AS listed
		WHERE (NOT CAST(@has_query AS boolean) OR listed.match_tier IS NOT NULL)
		  AND (
			(CAST(@sort_mode AS text) = 'heat' AND listed.heat_position IS NOT NULL)
			OR (CAST(@sort_mode AS text) = 'recommended' AND listed.recommendation_position IS NOT NULL)
			OR CAST(@sort_mode AS text) IN ('latest', 'relevance')
		  )
		  AND (
			NOT CAST(@has_pos AS boolean)
			OR (CAST(@sort_mode AS text) = 'heat' AND listed.heat_position > CAST(@cursor_pos AS integer))
			OR (CAST(@sort_mode AS text) = 'recommended' AND listed.recommendation_position > CAST(@cursor_pos AS integer))
		  )
		  AND (
			NOT CAST(@has_at AS boolean)
			OR (listed.first_published_at, listed.id) < (CAST(@cursor_at AS timestamptz), CAST(@cursor_id AS uuid))
		  )
		  AND (
			NOT CAST(@has_tier AS boolean)
			OR listed.match_tier > CAST(@cursor_tier AS integer)
			OR (
				listed.match_tier = CAST(@cursor_tier AS integer)
				AND listed.recommendation_score::numeric < CAST(@cursor_score AS numeric)
			)
			OR (
				listed.match_tier = CAST(@cursor_tier AS integer)
				AND listed.recommendation_score::numeric = CAST(@cursor_score AS numeric)
				AND listed.id < CAST(@cursor_id AS uuid)
			)
		  )
		ORDER BY
			CASE WHEN CAST(@sort_mode AS text) = 'relevance' THEN listed.match_tier END ASC,
			CASE WHEN CAST(@sort_mode AS text) = 'relevance' THEN listed.recommendation_score::numeric END DESC,
			CASE WHEN CAST(@sort_mode AS text) = 'heat' THEN listed.heat_position END ASC,
			CASE WHEN CAST(@sort_mode AS text) = 'recommended' THEN listed.recommendation_position END ASC,
			CASE WHEN CAST(@sort_mode AS text) = 'latest' THEN listed.first_published_at END DESC NULLS LAST,
			listed.id DESC
		LIMIT CAST(@row_limit AS integer)`
}

func tagCountSQL() string {
	return `
		SELECT t.id::text, t.name, t.slug, t.dimension, count(DISTINCT r.id)::bigint
		FROM tags t
		JOIN resource_tags rt ON rt.tag_id = t.id
		JOIN resources r ON r.id = rt.resource_id
		JOIN resource_publications p ON p.resource_id = r.id
		WHERE ` + publicWhere() + `
		  AND ` + textWhere() + `
		  AND (NOT CAST(@has_query AS boolean) OR (` + matchTierExpr() + `) IS NOT NULL)
		GROUP BY t.id, t.name, t.slug, t.dimension
		ORDER BY t.dimension, t.slug`
}

func detailSelect() string {
	return `
		SELECT
			r.id,
			r.kind,
			r.slug,
			p.title,
			p.summary,
			p.cover_urls,
			p.quality_score,
			r.first_published_at,
			p.content_updated_at,
			p.details,
			pc.id,
			pc.name,
			pc.slug,
			pc.dimension,
			` + tagJSON() + `,
			p.aliases,
			p.body_markdown,
			p.recommendation_reason
		FROM resources r
		JOIN resource_publications p ON p.resource_id = r.id
		LEFT JOIN tags pc ON pc.id = p.primary_category_id`
}

func detailByIDSQL() string {
	return detailSelect() + `
		WHERE r.id = CAST(@id AS uuid)
		  AND r.status = 'published'
		  AND (CAST(@include_demo AS boolean) OR NOT r.is_demo)`
}

func detailBySlugSQL() string {
	return detailSelect() + `
		WHERE r.slug = CAST(@slug AS text)
		  AND r.status = 'published'
		  AND (CAST(@include_demo AS boolean) OR NOT r.is_demo)
		ORDER BY r.first_published_at DESC NULLS LAST, r.id DESC
		LIMIT 1`
}

func featuredSQL() string {
	return `
		SELECT
			r.id,
			r.kind,
			r.slug,
			p.title,
			p.summary,
			p.cover_urls,
			p.quality_score,
			r.first_published_at,
			p.content_updated_at,
			p.details,
			pc.id,
			pc.name,
			pc.slug,
			pc.dimension,
			` + tagJSON() + `,
			s.position
		FROM featured_slots s
		JOIN resources r ON r.id = s.resource_id AND r.kind = s.kind
		JOIN resource_publications p ON p.resource_id = r.id
		LEFT JOIN tags pc ON pc.id = p.primary_category_id
		WHERE s.kind = CAST(@kind AS text)
		  AND s.placement = 'hero'
		  AND s.enabled
		  AND s.starts_at <= CAST(@now AS timestamptz)
		  AND (s.ends_at IS NULL OR s.ends_at > CAST(@now AS timestamptz))
		  AND r.status = 'published'
		  AND (CAST(@include_demo AS boolean) OR NOT r.is_demo)
		ORDER BY s.position ASC`
}
