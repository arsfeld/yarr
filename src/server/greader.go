package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nkanaev/yarr/src/server/auth"
	"github.com/nkanaev/yarr/src/server/router"
	"github.com/nkanaev/yarr/src/storage/model"
	"github.com/nkanaev/yarr/src/worker"
)

const (
	greaderItemIDPrefix   = "tag:google.com,2005:reader/item/"
	greaderLabelPrefix    = "user/-/label/"
	greaderFeedPrefix     = "feed/"
	greaderReadingList    = "user/-/state/com.google/reading-list"
	greaderRead           = "user/-/state/com.google/read"
	greaderStarred        = "user/-/state/com.google/starred"
	greaderActionTokenTTL = 30 * time.Minute
)

// --- Auth ---

func greaderAuthToken(username, password string) string {
	mac := hmac.New(sha256.New, []byte("yarr-greader:"+password))
	mac.Write([]byte(username))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) greaderAuth(c *router.Context) bool {
	if s.Username == "" && s.Password == "" {
		return false
	}

	header := c.Req.Header.Get("Authorization")
	if header == "" {
		return false
	}

	prefix := "GoogleLogin auth="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	token := strings.TrimPrefix(header, prefix)
	expected := greaderAuthToken(s.Username, s.Password)
	return auth.StringsEqual(token, expected)
}

func (s *Server) greaderValidateActionToken(c *router.Context) bool {
	token := c.Req.FormValue("T")
	if token == "" {
		return false
	}
	s.greaderTokensMutex.Lock()
	defer s.greaderTokensMutex.Unlock()

	expiry, ok := s.greaderTokens[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(s.greaderTokens, token)
		return false
	}
	return true
}

func greaderWriteUnauthorized(c *router.Context) {
	c.Out.Header().Set("X-Reader-Google-Bad-Token", "true")
	c.Out.WriteHeader(http.StatusUnauthorized)
}

func greaderWriteOK(c *router.Context) {
	c.Out.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Out.Write([]byte("OK"))
}

// --- ID encoding ---

func greaderItemIDToInt64(id string) (int64, error) {
	id = strings.TrimPrefix(id, greaderItemIDPrefix)
	return strconv.ParseInt(id, 16, 64)
}

func int64ToGreaderItemID(id int64) string {
	return fmt.Sprintf("%s%016x", greaderItemIDPrefix, id)
}

// --- Stream parsing ---

type greaderStreamType int

const (
	streamReadingList greaderStreamType = iota
	streamRead
	streamStarred
	streamFeed
	streamLabel
	streamUnknown
)

type greaderStream struct {
	Type greaderStreamType
	ID   string // feed ID (numeric string) or label name
}

func parseGreaderStream(s string) greaderStream {
	switch {
	case s == greaderReadingList:
		return greaderStream{Type: streamReadingList}
	case s == greaderRead:
		return greaderStream{Type: streamRead}
	case s == greaderStarred:
		return greaderStream{Type: streamStarred}
	case strings.HasPrefix(s, greaderFeedPrefix):
		return greaderStream{Type: streamFeed, ID: strings.TrimPrefix(s, greaderFeedPrefix)}
	case strings.HasPrefix(s, greaderLabelPrefix):
		return greaderStream{Type: streamLabel, ID: strings.TrimPrefix(s, greaderLabelPrefix)}
	default:
		return greaderStream{Type: streamUnknown}
	}
}

// --- ClientLogin handler ---

func (s *Server) handleGReaderLogin(c *router.Context) {
	if c.Req.Method != "POST" {
		c.Out.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.Username == "" && s.Password == "" {
		c.Out.WriteHeader(http.StatusUnauthorized)
		c.Out.Write([]byte("Error=AuthNotConfigured\n"))
		return
	}

	c.Req.ParseForm()
	email := c.Req.FormValue("Email")
	passwd := c.Req.FormValue("Passwd")

	if !auth.StringsEqual(email, s.Username) || !auth.StringsEqual(passwd, s.Password) {
		c.Out.WriteHeader(http.StatusUnauthorized)
		c.Out.Write([]byte("Error=BadAuthentication\n"))
		return
	}

	token := greaderAuthToken(s.Username, s.Password)
	c.Out.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(c.Out, "SID=%s\nLSID=%s\nAuth=%s\n", token, token, token)
}

// --- Main dispatcher ---

func (s *Server) handleGReader(c *router.Context) {
	path := c.Vars["path"]

	// Token endpoint doesn't need action token validation
	if path == "api/0/token" {
		if !s.greaderAuth(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderToken(c)
		return
	}

	// All other endpoints require auth
	if !s.greaderAuth(c) {
		greaderWriteUnauthorized(c)
		return
	}

	switch path {
	// Read endpoints (GET)
	case "api/0/tag/list":
		s.handleGReaderTagList(c)
	case "api/0/subscription/list":
		s.handleGReaderSubscriptionList(c)
	case "api/0/stream/items/ids":
		s.handleGReaderStreamItemIDs(c)
	case "api/0/stream/items/contents":
		s.handleGReaderStreamContents(c)
	case "api/0/unread-count":
		s.handleGReaderUnreadCount(c)

	// Write endpoints (POST, require action token)
	case "api/0/edit-tag":
		if !s.greaderValidateActionToken(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderEditTag(c)
	case "api/0/mark-all-as-read":
		if !s.greaderValidateActionToken(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderMarkAllAsRead(c)
	case "api/0/subscription/quickadd":
		if !s.greaderValidateActionToken(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderSubscriptionQuickAdd(c)
	case "api/0/subscription/edit":
		if !s.greaderValidateActionToken(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderSubscriptionEdit(c)
	case "api/0/rename-tag":
		if !s.greaderValidateActionToken(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderRenameTag(c)
	case "api/0/disable-tag":
		if !s.greaderValidateActionToken(c) {
			greaderWriteUnauthorized(c)
			return
		}
		s.handleGReaderDisableTag(c)

	default:
		c.Out.WriteHeader(http.StatusNotFound)
	}
}

// --- Token ---

func (s *Server) handleGReaderToken(c *router.Context) {
	b := make([]byte, 16)
	rand.Read(b)
	token := hex.EncodeToString(b)

	s.greaderTokensMutex.Lock()
	s.greaderTokens[token] = time.Now().Add(greaderActionTokenTTL)
	s.greaderTokensMutex.Unlock()

	c.Out.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Out.Write([]byte(token))
}

// --- Phase 2: Read Path ---

func (s *Server) handleGReaderTagList(c *router.Context) {
	folders := s.db.ListFolders()

	type tag struct {
		ID string `json:"id"`
	}

	tags := make([]tag, 0, len(folders)+1)
	tags = append(tags, tag{ID: greaderStarred})
	for _, folder := range folders {
		tags = append(tags, tag{ID: greaderLabelPrefix + folder.Title})
	}

	c.JSON(http.StatusOK, map[string]interface{}{"tags": tags})
}

func (s *Server) handleGReaderSubscriptionList(c *router.Context) {
	feeds := s.db.ListFeeds()
	folders := s.db.ListFolders()

	folderMap := make(map[int64]string)
	for _, folder := range folders {
		folderMap[folder.Id] = folder.Title
	}

	type category struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	type subscription struct {
		ID         string     `json:"id"`
		Title      string     `json:"title"`
		URL        string     `json:"url"`
		HTMLURL    string     `json:"htmlUrl"`
		IconURL    string     `json:"iconUrl"`
		Categories []category `json:"categories"`
	}

	subs := make([]subscription, len(feeds))
	for i, feed := range feeds {
		cats := make([]category, 0)
		if feed.FolderId != nil {
			if title, ok := folderMap[*feed.FolderId]; ok {
				cats = append(cats, category{
					ID:    greaderLabelPrefix + title,
					Label: title,
				})
			}
		}
		subs[i] = subscription{
			ID:         fmt.Sprintf("feed/%d", feed.Id),
			Title:      feed.Title,
			URL:        feed.FeedLink,
			HTMLURL:    feed.Link,
			IconURL:    "",
			Categories: cats,
		}
	}

	c.JSON(http.StatusOK, map[string]interface{}{"subscriptions": subs})
}

func (s *Server) handleGReaderStreamItemIDs(c *router.Context) {
	query := c.Req.URL.Query()
	streamID := query.Get("s")
	excludeTag := query.Get("xt")
	countStr := query.Get("n")
	otStr := query.Get("ot")
	continuation := query.Get("c")

	count := 1000
	if countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 {
			count = n
		}
	}
	if count > 10000 {
		count = 10000
	}

	filter := model.ItemFilter{}
	stream := parseGreaderStream(streamID)

	switch stream.Type {
	case streamReadingList:
		// all items, no filter
	case streamStarred:
		status := model.STARRED
		filter.Status = &status
	case streamFeed:
		if feedID, err := strconv.ParseInt(stream.ID, 10, 64); err == nil {
			filter.FeedID = &feedID
		}
	case streamLabel:
		folder := s.db.GetFolderByTitle(stream.ID)
		if folder != nil {
			filter.FolderID = &folder.Id
		}
	default:
		c.JSON(http.StatusOK, map[string]interface{}{"itemRefs": []interface{}{}})
		return
	}

	// Handle exclude tag
	if excludeTag == greaderRead {
		// Exclude read items: show unread + starred
		// Since we can't do OR in ItemFilter, and starred items should appear as unread,
		// we filter for UNREAD status. However, starred items need to be included too
		// unless we're already filtering for starred.
		if filter.Status == nil {
			status := model.UNREAD
			filter.Status = &status
		}
	}

	// Handle oldest timestamp
	if otStr != "" {
		if ot, err := strconv.ParseInt(otStr, 10, 64); err == nil {
			t := time.Unix(ot, 0)
			filter.AfterDate = &t
		}
	}

	// Handle continuation (cursor-based pagination using MaxID)
	if continuation != "" {
		if maxID, err := strconv.ParseInt(continuation, 10, 64); err == nil {
			filter.MaxID = &maxID
		}
	}

	// Fetch count+1 to detect if there are more results
	ids := s.db.ListItemIDs(filter, count+1, true)

	// If we're excluding read items, we also need starred items
	if excludeTag == greaderRead && filter.Status != nil {
		starredStatus := model.STARRED
		starredFilter := model.ItemFilter{Status: &starredStatus}
		if filter.FeedID != nil {
			starredFilter.FeedID = filter.FeedID
		}
		if filter.FolderID != nil {
			starredFilter.FolderID = filter.FolderID
		}
		if filter.MaxID != nil {
			starredFilter.MaxID = filter.MaxID
		}
		if filter.AfterDate != nil {
			starredFilter.AfterDate = filter.AfterDate
		}
		starredIDs := s.db.ListItemIDs(starredFilter, count+1, true)

		// Merge and deduplicate
		idSet := make(map[int64]bool)
		for _, id := range ids {
			idSet[id] = true
		}
		for _, id := range starredIDs {
			idSet[id] = true
		}
		ids = make([]int64, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		// Sort descending (newest first)
		sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	}

	type itemRef struct {
		ID string `json:"id"`
	}

	hasMore := len(ids) > count
	if hasMore {
		ids = ids[:count]
	}

	refs := make([]itemRef, len(ids))
	for i, id := range ids {
		refs[i] = itemRef{ID: strconv.FormatInt(id, 10)}
	}

	result := map[string]interface{}{"itemRefs": refs}
	if hasMore && len(ids) > 0 {
		result["continuation"] = strconv.FormatInt(ids[len(ids)-1], 10)
	}

	c.JSON(http.StatusOK, result)
}

func (s *Server) handleGReaderStreamContents(c *router.Context) {
	c.Req.ParseForm()

	itemIDs := c.Req.Form["i"]
	if len(itemIDs) == 0 {
		c.JSON(http.StatusOK, map[string]interface{}{
			"id":    greaderReadingList,
			"items": []interface{}{},
		})
		return
	}

	// Parse item IDs from hex format
	int64IDs := make([]int64, 0, len(itemIDs))
	for _, idStr := range itemIDs {
		if id, err := greaderItemIDToInt64(idStr); err == nil {
			int64IDs = append(int64IDs, id)
		}
	}

	// Fetch items
	filter := model.ItemFilter{IDs: &int64IDs}
	items := s.db.ListItems(filter, len(int64IDs), true, true)

	// Build feed title map for origin
	feeds := s.db.ListFeeds()
	feedMap := make(map[int64]model.Feed)
	for _, feed := range feeds {
		feedMap[feed.Id] = feed
	}

	folders := s.db.ListFolders()
	folderMap := make(map[int64]string)
	for _, folder := range folders {
		folderMap[folder.Id] = folder.Title
	}

	type summary struct {
		Content string `json:"content"`
	}
	type alternate struct {
		Href string `json:"href"`
	}
	type origin struct {
		StreamID string `json:"streamId"`
		Title    string `json:"title"`
	}
	type entry struct {
		ID            string      `json:"id"`
		Title         string      `json:"title"`
		Author        string      `json:"author"`
		Published     int64       `json:"published"`
		CrawlTimeMsec string      `json:"crawlTimeMsec"`
		TimestampUsec string      `json:"timestampUsec"`
		Summary       summary     `json:"summary"`
		Alternate     []alternate `json:"alternate"`
		Categories    []string    `json:"categories"`
		Origin        origin      `json:"origin"`
	}

	entries := make([]entry, 0, len(items))
	for _, item := range items {
		cats := []string{greaderReadingList}
		if item.Status == model.READ || item.Status == model.STARRED {
			cats = append(cats, greaderRead)
		}
		if item.Status == model.STARRED {
			cats = append(cats, greaderStarred)
		}

		feed := feedMap[item.FeedId]
		if feed.FolderId != nil {
			if title, ok := folderMap[*feed.FolderId]; ok {
				cats = append(cats, greaderLabelPrefix+title)
			}
		}

		ts := item.Date.Unix()
		tsMs := item.Date.UnixMilli()
		tsUs := item.Date.UnixMicro()

		entries = append(entries, entry{
			ID:            int64ToGreaderItemID(item.Id),
			Title:         item.Title,
			Published:     ts,
			CrawlTimeMsec: strconv.FormatInt(tsMs, 10),
			TimestampUsec: strconv.FormatInt(tsUs, 10),
			Summary:       summary{Content: item.Content},
			Alternate:     []alternate{{Href: item.Link}},
			Categories:    cats,
			Origin: origin{
				StreamID: fmt.Sprintf("feed/%d", item.FeedId),
				Title:    feed.Title,
			},
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"id":      greaderReadingList,
		"updated": time.Now().Unix(),
		"items":   entries,
	})
}

func (s *Server) handleGReaderUnreadCount(c *router.Context) {
	feeds := s.db.ListFeeds()
	stats := s.db.FeedStats()

	statMap := make(map[int64]model.FeedStat)
	for _, stat := range stats {
		statMap[stat.FeedId] = stat
	}

	type unreadCount struct {
		ID                      string `json:"id"`
		Count                   int64  `json:"count"`
		NewestItemTimestampUsec string `json:"newestItemTimestampUsec"`
	}

	counts := make([]unreadCount, 0)
	var totalUnread int64

	for _, feed := range feeds {
		stat := statMap[feed.Id]
		feedUnread := stat.UnreadCount + stat.StarredCount
		if feedUnread > 0 {
			counts = append(counts, unreadCount{
				ID:                      fmt.Sprintf("feed/%d", feed.Id),
				Count:                   feedUnread,
				NewestItemTimestampUsec: strconv.FormatInt(time.Now().UnixMicro(), 10),
			})
		}
		totalUnread += feedUnread
	}

	if totalUnread > 0 {
		counts = append(counts, unreadCount{
			ID:                      greaderReadingList,
			Count:                   totalUnread,
			NewestItemTimestampUsec: strconv.FormatInt(time.Now().UnixMicro(), 10),
		})
	}

	c.JSON(http.StatusOK, map[string]interface{}{
		"max":          1000,
		"unreadcounts": counts,
	})
}

// --- Phase 3: Write Path ---

func (s *Server) handleGReaderEditTag(c *router.Context) {
	c.Req.ParseForm()

	itemIDs := c.Req.Form["i"]
	addTag := c.Req.FormValue("a")
	removeTag := c.Req.FormValue("r")

	for _, idStr := range itemIDs {
		id, err := greaderItemIDToInt64(idStr)
		if err != nil {
			continue
		}

		item := s.db.GetItem(id)
		if item == nil {
			continue
		}

		switch {
		case addTag == greaderStarred:
			s.db.UpdateItemStatus(id, model.STARRED)
		case removeTag == greaderStarred:
			s.db.UpdateItemStatus(id, model.READ)
		case addTag == greaderRead:
			if item.Status != model.STARRED {
				s.db.UpdateItemStatus(id, model.READ)
			}
		case removeTag == greaderRead:
			if item.Status != model.STARRED {
				s.db.UpdateItemStatus(id, model.UNREAD)
			}
		}
	}

	greaderWriteOK(c)
}

func (s *Server) handleGReaderMarkAllAsRead(c *router.Context) {
	c.Req.ParseForm()

	streamID := c.Req.FormValue("s")
	tsStr := c.Req.FormValue("ts")

	filter := model.MarkFilter{}
	stream := parseGreaderStream(streamID)

	switch stream.Type {
	case streamReadingList:
		// all items, no filter
	case streamFeed:
		if feedID, err := strconv.ParseInt(stream.ID, 10, 64); err == nil {
			filter.FeedID = &feedID
		}
	case streamLabel:
		folder := s.db.GetFolderByTitle(stream.ID)
		if folder != nil {
			filter.FolderID = &folder.Id
		}
	default:
		c.Out.WriteHeader(http.StatusBadRequest)
		return
	}

	if tsStr != "" {
		if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			// ts is in microseconds
			before := time.Unix(ts/1000000, (ts%1000000)*1000)
			filter.Before = &before
		}
	}

	s.db.MarkItemsRead(filter)
	greaderWriteOK(c)
}

// --- Phase 4: Feed Management ---

func (s *Server) handleGReaderSubscriptionQuickAdd(c *router.Context) {
	c.Req.ParseForm()

	feedURL := c.Req.FormValue("quickadd")
	if feedURL == "" {
		c.Out.WriteHeader(http.StatusBadRequest)
		return
	}

	result, err := worker.DiscoverFeed(feedURL)
	if err != nil || (result.Feed == nil && len(result.Sources) == 0) {
		c.JSON(http.StatusOK, map[string]interface{}{
			"numResults": 0,
		})
		return
	}

	// If multiple sources, pick the first one and re-discover
	if result.Feed == nil && len(result.Sources) > 0 {
		result, err = worker.DiscoverFeed(result.Sources[0].URL)
		if err != nil || result.Feed == nil {
			c.JSON(http.StatusOK, map[string]interface{}{
				"numResults": 0,
			})
			return
		}
	}

	feed := s.db.CreateFeed(model.CreateFeedParams{
		Title:    result.Feed.Title,
		Link:     result.Feed.SiteURL,
		FeedLink: result.FeedLink,
	})
	if feed == nil {
		c.JSON(http.StatusOK, map[string]interface{}{
			"numResults": 0,
		})
		return
	}

	items := worker.ConvertItems(result.Feed.Items, *feed)
	if len(items) > 0 {
		s.db.CreateItems(items)
	}
	s.worker.FindFeedFavicon(*feed)

	c.JSON(http.StatusOK, map[string]interface{}{
		"numResults": 1,
		"streamId":   fmt.Sprintf("feed/%d", feed.Id),
	})
}

func (s *Server) handleGReaderSubscriptionEdit(c *router.Context) {
	c.Req.ParseForm()

	action := c.Req.FormValue("ac")
	streamID := c.Req.FormValue("s")
	title := c.Req.FormValue("t")
	addLabel := c.Req.FormValue("a")
	removeLabel := c.Req.FormValue("r")

	switch action {
	case "subscribe":
		feedURL := strings.TrimPrefix(streamID, greaderFeedPrefix)
		result, err := worker.DiscoverFeed(feedURL)
		if err != nil || result.Feed == nil {
			if result != nil && len(result.Sources) > 0 {
				result, err = worker.DiscoverFeed(result.Sources[0].URL)
				if err != nil || result.Feed == nil {
					c.Out.WriteHeader(http.StatusBadRequest)
					return
				}
			} else {
				c.Out.WriteHeader(http.StatusBadRequest)
				return
			}
		}

		var folderID *int64
		if addLabel != "" {
			labelName := strings.TrimPrefix(addLabel, greaderLabelPrefix)
			folder := s.db.GetFolderByTitle(labelName)
			if folder == nil {
				folder = s.db.CreateFolder(labelName)
			}
			if folder != nil {
				folderID = &folder.Id
			}
		}

		feedTitle := result.Feed.Title
		if title != "" {
			feedTitle = title
		}

		feed := s.db.CreateFeed(model.CreateFeedParams{
			Title:    feedTitle,
			Link:     result.Feed.SiteURL,
			FeedLink: result.FeedLink,
			FolderID: folderID,
		})
		if feed != nil {
			items := worker.ConvertItems(result.Feed.Items, *feed)
			if len(items) > 0 {
				s.db.CreateItems(items)
			}
			s.worker.FindFeedFavicon(*feed)
		}

	case "unsubscribe":
		feedIDStr := strings.TrimPrefix(streamID, greaderFeedPrefix)
		if feedID, err := strconv.ParseInt(feedIDStr, 10, 64); err == nil {
			s.db.DeleteFeed(feedID)
		}

	case "edit":
		feedIDStr := strings.TrimPrefix(streamID, greaderFeedPrefix)
		feedID, err := strconv.ParseInt(feedIDStr, 10, 64)
		if err != nil {
			c.Out.WriteHeader(http.StatusBadRequest)
			return
		}

		if title != "" {
			s.db.UpdateFeed(feedID, model.UpdateFeedParams{Title: &title})
		}

		if addLabel != "" {
			labelName := strings.TrimPrefix(addLabel, greaderLabelPrefix)
			folder := s.db.GetFolderByTitle(labelName)
			if folder == nil {
				folder = s.db.CreateFolder(labelName)
			}
			if folder != nil {
				s.db.UpdateFeed(feedID, model.UpdateFeedParams{
					FolderID: model.SetNullable(&folder.Id),
				})
			}
		} else if removeLabel != "" {
			s.db.UpdateFeed(feedID, model.UpdateFeedParams{
				FolderID: model.SetNullable[int64](nil),
			})
		}
	default:
		c.Out.WriteHeader(http.StatusBadRequest)
		return
	}

	greaderWriteOK(c)
}

func (s *Server) handleGReaderRenameTag(c *router.Context) {
	c.Req.ParseForm()

	source := c.Req.FormValue("s")
	dest := c.Req.FormValue("dest")

	oldName := strings.TrimPrefix(source, greaderLabelPrefix)
	newName := strings.TrimPrefix(dest, greaderLabelPrefix)

	folder := s.db.GetFolderByTitle(oldName)
	if folder == nil {
		c.Out.WriteHeader(http.StatusNotFound)
		return
	}

	s.db.UpdateFolder(folder.Id, model.UpdateFolderParams{Title: &newName})
	greaderWriteOK(c)
}

func (s *Server) handleGReaderDisableTag(c *router.Context) {
	c.Req.ParseForm()

	source := c.Req.FormValue("s")
	folderName := strings.TrimPrefix(source, greaderLabelPrefix)

	folder := s.db.GetFolderByTitle(folderName)
	if folder == nil {
		c.Out.WriteHeader(http.StatusNotFound)
		return
	}

	s.db.DeleteFolder(folder.Id)
	greaderWriteOK(c)
}
