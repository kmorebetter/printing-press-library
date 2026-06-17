// Copyright 2026 Maxime Delavergne and contributors. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/medium-reader/internal/cliutil"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/medium-reader/internal/store"
	"github.com/spf13/cobra"
)

// pp:data-source live
// author-archive mirrors a writer's entire body of work into the local SQLite
// store, full-text searchable offline. v2 sources it directly from Medium's own
// internal GraphQL endpoint (the id list, via resolver.AuthorArchive) plus the
// article-page surface (each article's Markdown body, via resolver.ReadArticle)
// — no API key, no RapidAPI, no cookies required. It is the population path the
// store-backed commands (corpus, digest, author-compare) read from.
func newNovelAuthorArchiveCmd(flags *rootFlags) *cobra.Command {
	var maxArticles int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "author-archive <user-id>",
		Short: "Mirror a writer's entire body of work into local SQLite, full-text searchable offline.",
		Long: strings.Trim(`
Mirror a Medium writer's articles into the local SQLite store for offline,
full-text search. Sourced directly from Medium's internal GraphQL endpoint (the
article id list) and the article-page surface (each body), with no API key and
no cookies.

The argument is the writer's Medium user id (the stable hex id, e.g.
bcab753a4d4e) OR their @handle / username (e.g. @quincylarson or quincylarson). A
handle is resolved to its user id keylessly, by reading the author's public
profile page — no API key, no cookies. You can also find a user id from a feed
result's author_id field.`, "\n"),
		Example: strings.Trim(`
  medium-reader-pp-cli author-archive bcab753a4d4e --max-articles 25 --agent
  medium-reader-pp-cli author-archive @quincylarson --max-articles 25 --agent
  medium-reader-pp-cli author-archive bcab753a4d4e --dry-run
`, "\n"),
		Annotations: map[string]string{"mcp:read-only": "true"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && cmd.Flags().NFlag() == 0 {
				return cmd.Help()
			}
			userID := ""
			if len(args) > 0 {
				userID = strings.TrimSpace(args[0])
			}
			if maxArticles <= 0 {
				maxArticles = 25
			}
			// Dogfood matrix bounds the crawl so the per-command timeout holds.
			if cliutil.IsDogfoodEnv() && maxArticles > 2 {
				maxArticles = 2
			}
			if dryRunOK(flags) {
				target := userID
				if target == "" {
					target = "<user-id>"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "would archive up to %d articles for user %s\n", maxArticles, target)
				return nil
			}
			if userID == "" {
				_ = cmd.Usage()
				return usageErr(fmt.Errorf("<user-id> is required"))
			}

			if dbPath == "" {
				dbPath = defaultDBPath("medium-reader-pp-cli")
			}

			ctx, cancel := boundCtx(cmd.Context(), flags)
			defer cancel()

			// Accept a @handle / username as well as a raw user id. The
			// GraphQL archive surface needs the stable hex id, so resolve a
			// handle to its id keylessly via the public profile page (v1's
			// id_for behavior, without the API key). A bare hex id is used
			// as-is.
			if !looksLikeUserID(userID) {
				handle := userID
				resolvedID, rerr := flags.newPageSource().ResolveUserID(ctx, handle)
				if rerr != nil {
					return usageErr(fmt.Errorf("author-archive %q: could not resolve handle to a user id (pass the 12-hex user id, e.g. bcab753a4d4e, or check the @handle): %w", handle, rerr))
				}
				userID = resolvedID
			}

			resolver := flags.newResolver()

			// 1. List the writer's article summaries (ids + metadata) via the
			//    GraphQL author-archive surface. A surface outage degrades to the
			//    typed ErrSurfaceUnavailable rather than crashing.
			summaries, err := resolver.AuthorArchive(ctx, userID, maxArticles)
			if err != nil {
				return apiErr(fmt.Errorf("author-archive %q: %w", userID, err))
			}
			if len(summaries) > maxArticles {
				summaries = summaries[:maxArticles]
			}

			// 2. Open the local store for upserts.
			db, err := store.OpenWithContext(ctx, dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			authorName := userID
			archived := 0
			for _, s := range summaries {
				if err := ctx.Err(); err != nil {
					break
				}
				if s.ID == "" {
					continue
				}
				if s.Author != "" {
					authorName = s.Author
				}

				// Build the stored record from the summary metadata, enriching
				// with the full Markdown body via the read surface (best-effort:
				// a missing body still archives the metadata).
				obj := map[string]any{
					"id":              s.ID,
					"title":           s.Title,
					"url":             s.URL,
					"author_name":     s.Author,
					"author_id":       s.AuthorID,
					"username":        s.Username,
					"archived_author": userID,
				}
				if !s.PublishedAt.IsZero() {
					obj["first_published_at"] = s.PublishedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
				}

				art, rerr := resolver.ReadArticle(ctx, s.ID)
				if rerr == nil && art != nil {
					if art.Markdown != "" {
						obj["markdown"] = art.Markdown
					}
					if art.Subtitle != "" {
						obj["subtitle"] = art.Subtitle
					}
					obj["is_locked"] = art.IsLocked
					obj["is_preview_only"] = art.IsPreviewOnly
					if art.WordCount > 0 {
						obj["word_count"] = art.WordCount
					}
				} else if rerr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: article %s: body unavailable: %v\n", s.ID, rerr)
				}

				merged, merr := json.Marshal(obj)
				if merr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: article %s: marshal: %v\n", s.ID, merr)
					continue
				}
				if err := db.Upsert("articles", s.ID, merged); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: article %s: store: %v\n", s.ID, err)
					continue
				}
				archived++
			}

			summary := map[string]any{
				"author":   authorName,
				"user_id":  userID,
				"archived": archived,
				"db_path":  dbPath,
			}
			return printJSONFiltered(cmd.OutOrStdout(), summary, flags)
		},
	}
	cmd.Flags().IntVar(&maxArticles, "max-articles", 25, "Maximum number of articles to archive")
	cmd.Flags().StringVar(&dbPath, "db", "", "Path to the local SQLite store (default: standard data dir)")
	return cmd
}

// looksLikeUserID reports whether arg is already a Medium user id rather than a
// handle/username that needs resolving. Medium user ids are lowercase-hex runs
// (the canonical form is 12 chars, e.g. bcab753a4d4e); we accept a 10-16-char
// all-hex token to stay robust to id-length drift. A leading "@" is always a
// handle, never an id, so it short-circuits to false.
func looksLikeUserID(arg string) bool {
	s := strings.TrimSpace(arg)
	if s == "" || strings.HasPrefix(s, "@") {
		return false
	}
	if len(s) < 10 || len(s) > 16 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
