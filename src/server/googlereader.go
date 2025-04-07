package server

import (
	"crypto/md5"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nkanaev/yarr/src/server/auth"
	"github.com/nkanaev/yarr/src/server/router"
	"github.com/nkanaev/yarr/src/storage"
)

// StreamPrefix is the prefix for streams (read/starred/reading list)
const StreamPrefix = "user/-/state/com.google/"

// UserStreamPrefix is the user specific prefix for streams
const UserStreamPrefix = "user/%d/state/com.google/"

// LabelPrefix is the prefix for a label stream
const LabelPrefix = "user/-/label/"

// UserLabelPrefix is the user specific prefix for a label stream
const UserLabelPrefix = "user/%d/label/"

// FeedPrefix is the prefix for a feed stream
const FeedPrefix = "feed/"

// Stream types
const (
	Read               = "read"
	Starred            = "starred"
	ReadingList        = "reading-list"
	KeptUnread         = "kept-unread"
	Fresh              = "fresh"
	All                = "all"
	Like               = "like"
	Broadcast          = "broadcast"
	RecommendedSources = "recommended-sources"
	BlogSearch         = "blog-search"
	PopularItems       = "pop"
	Comments           = "comments"
	Created            = "created"
	Shared             = "shared"
	Notes              = "notes"
)

// Short entry ID format
const EntryIDShort = "%016x"

// Long entry ID format
const EntryIDLong = "tag:google.com,2005:reader/item/%016x"

// Default values and limits
const (
	defaultUserID    int64 = 1
	defaultItemCount       = 1000
	maxItemCount           = 10000
)

// Request parameters for stream operations
type streamParams struct {
	count        int
	excludeTags  []string
	includeTags  []string
	continuation string
	olderThan    *time.Time
	newerThan    *time.Time
	oldestFirst  bool
}

// Stream ID generators
func userStreamID(userID int64, streamType string) string {
	return fmt.Sprintf(UserStreamPrefix, userID) + streamType
}

func userLabelID(userID int64, label string) string {
	return fmt.Sprintf(UserLabelPrefix, userID) + label
}

func feedStreamID(feedID int64) string {
	return fmt.Sprintf(FeedPrefix+"%d", feedID)
}

// Login response
type login struct {
	SID  string
	LSID string
	Auth string
}

func (l *login) String() string {
	return fmt.Sprintf("SID=%s\nLSID=%s\nAuth=%s", l.SID, l.LSID, l.Auth)
}

// User info response
type userInfo struct {
	UserID        string `json:"userId"`
	UserName      string `json:"userName"`
	UserProfileID string `json:"userProfileId"`
	UserEmail     string `json:"userEmail"`
}

// Tag list response
type tagsResponse struct {
	Tags []subscriptionCategory `json:"tags" xml:"list>object"`
}

// Subscription category
type subscriptionCategory struct {
	ID     string `json:"id" xml:"string"`
	SortID string `json:"sortid,omitempty" xml:"sortid,omitempty"`
	Label  string `json:"label,omitempty" xml:"label,omitempty"`
	Type   string `json:"type,omitempty" xml:"type,omitempty"`
}

// Subscription response
type subscriptionsResponse struct {
	Subscriptions []subscription `json:"subscriptions" xml:"list>object"`
}

// Subscription
type subscription struct {
	ID         string                 `json:"id" xml:"string"`
	Title      string                 `json:"title" xml:"title"`
	Categories []subscriptionCategory `json:"categories" xml:"categories>object"`
	URL        string                 `json:"url" xml:"url"`
	HTMLURL    string                 `json:"htmlUrl" xml:"htmlUrl"`
	IconURL    string                 `json:"iconUrl" xml:"iconUrl"`
}

// Item reference for stream/items/ids response
type itemRefID struct {
	ID string `json:"id"` // Plain decimal ID as string
}

// Stream item ID response
type streamIDResponse struct {
	ItemRefs     []itemRefID `json:"itemRefs" xml:"itemRefs>object"`
	Continuation string      `json:"continuation,omitempty" xml:"continuation,omitempty"`
}

// Quick add response
type quickAddResponse struct {
	NumResults int    `json:"numResults"`
	Query      string `json:"query,omitempty"`
	StreamID   string `json:"streamId,omitempty"`
	StreamName string `json:"streamName,omitempty"`
}

// Stream content items
type streamContentItems struct {
	Direction    string            `json:"direction" xml:"direction"`
	ID           string            `json:"id" xml:"id"`
	Title        string            `json:"title" xml:"title"`
	Author       string            `json:"author" xml:"author"`
	Updated      int64             `json:"updated" xml:"updated"`
	Items        []contentItem     `json:"items" xml:"items>item"`
	Self         []contentHREF     `json:"self" xml:"self>href"`
	Alternate    []contentHREFType `json:"alternate" xml:"alternate>link"`
	Continuation string            `json:"continuation,omitempty" xml:"continuation,omitempty"`
}

// Content HREF
type contentHREF struct {
	HREF string `json:"href"`
}

// Content HREF with type
type contentHREFType struct {
	HREF string `json:"href"`
	Type string `json:"type"`
}

// Content item
type contentItem struct {
	ID            string                 `json:"id" xml:"id"`
	Title         string                 `json:"title" xml:"title"`
	Author        string                 `json:"author,omitempty" xml:"author,omitempty"`
	Categories    []string               `json:"categories" xml:"categories>string"`
	Published     int64                  `json:"published,omitempty" xml:"published,omitempty"`
	Updated       int64                  `json:"updated,omitempty" xml:"updated,omitempty"`
	Alternate     []contentHREFType      `json:"alternate" xml:"alternate>link"`
	Content       contentItemContent     `json:"content" xml:"content"`
	Summary       contentItemContent     `json:"summary" xml:"summary"`
	Origin        contentItemOrigin      `json:"origin" xml:"origin"`
	Canonical     []contentHREF          `json:"canonical,omitempty" xml:"canonical>href,omitempty"`
	TimestampUsec string                 `json:"timestampUsec" xml:"timestampUsec"`
	CrawlTimeMsec string                 `json:"crawlTimeMsec" xml:"crawlTimeMsec"`
	Enclosure     []contentItemEnclosure `json:"enclosure,omitempty" xml:"enclosure>link,omitempty"`
}

// Content item content
type contentItemContent struct {
	Direction string `json:"direction" xml:"direction"`
	Content   string `json:"content" xml:"content"`
}

// Content item origin
type contentItemOrigin struct {
	StreamID string `json:"streamId" xml:"streamId"`
	Title    string `json:"title" xml:"title"`
	HTMLUrl  string `json:"htmlUrl" xml:"htmlUrl"`
}

// Content item enclosure
type contentItemEnclosure struct {
	URL  string `json:"url" xml:"url"`
	Type string `json:"type" xml:"type"`
}

// Stream type
type StreamType int

const (
	NoStream StreamType = iota
	ReadStream
	StarredStream
	ReadingListStream
	KeptUnreadStream
	LabelStream
	FeedStream
)

// Stream defines a stream type and its ID
type Stream struct {
	Type StreamType
	ID   string
}

// Unread count response
type unreadCountResponse struct {
	Max    int           `json:"max" xml:"max"`
	Unread int           `json:"unreadcounts" xml:"unreadcounts"`
	Counts []unreadCount `json:"unreadcounts" xml:"unreadcounts>object"`
}

type unreadCount struct {
	ID         string `json:"id" xml:"string"`
	Count      int    `json:"count" xml:"count"`
	NewestItem int64  `json:"newestItemTimestampUsec" xml:"newestItemTimestampUsec"`
}

// OK writes a standard "OK" response
func OK(c *router.Context) {
	c.JSON(http.StatusOK, map[string]interface{}{
		"status": "ok",
	})
}

// Generate an auth token for a user
func getAuthToken(username, password string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(username+":"+password)))
}

// Parse a stream ID into a Stream object
func getStream(streamID string, userID int64) (Stream, error) {
	log.Printf("[GReader] Parsing stream ID: %s for user ID: %d", streamID, userID)

	switch {
	case strings.HasPrefix(streamID, FeedPrefix):
		feedID := strings.TrimPrefix(streamID, FeedPrefix)
		log.Printf("[GReader] Identified feed stream with ID: %s", feedID)
		return Stream{Type: FeedStream, ID: feedID}, nil

	case strings.HasPrefix(streamID, fmt.Sprintf(UserStreamPrefix, userID)) || strings.HasPrefix(streamID, StreamPrefix):
		id := strings.TrimPrefix(streamID, fmt.Sprintf(UserStreamPrefix, userID))
		id = strings.TrimPrefix(id, StreamPrefix)
		log.Printf("[GReader] Processing user stream with ID: %s", id)

		switch id {
		case Read:
			log.Printf("[GReader] Identified read stream")
			return Stream{ReadStream, ""}, nil
		case Starred:
			log.Printf("[GReader] Identified starred stream")
			return Stream{StarredStream, ""}, nil
		case ReadingList:
			log.Printf("[GReader] Identified reading list stream")
			return Stream{ReadingListStream, ""}, nil
		default:
			log.Printf("[GReader] Error: Unknown stream with ID: %s", id)
			return Stream{NoStream, ""}, fmt.Errorf("unknown stream with id: %s", id)
		}

	case strings.HasPrefix(streamID, fmt.Sprintf(UserLabelPrefix, userID)) || strings.HasPrefix(streamID, LabelPrefix):
		id := strings.TrimPrefix(streamID, fmt.Sprintf(UserLabelPrefix, userID))
		id = strings.TrimPrefix(id, LabelPrefix)
		log.Printf("[GReader] Identified label stream with ID: %s", id)
		return Stream{LabelStream, id}, nil

	case streamID == "":
		log.Printf("[GReader] Error: Empty stream ID")
		return Stream{NoStream, ""}, nil

	default:
		log.Printf("[GReader] Error: Unknown stream type for ID: %s", streamID)
		return Stream{NoStream, ""}, fmt.Errorf("unknown stream type: %s", streamID)
	}
}

// parseStreamParams extracts common stream operation parameters from a request
func parseStreamParams(r *http.Request) streamParams {
	params := streamParams{
		count: defaultItemCount,
	}

	// Helper to get param from Form first, then Query
	getParam := func(key string) string {
		if val := r.FormValue(key); val != "" {
			return val
		}
		return r.URL.Query().Get(key)
	}

	// Parse count
	if countStr := getParam("n"); countStr != "" {
		if countVal, err := strconv.Atoi(countStr); err == nil {
			if countVal > 0 && countVal <= maxItemCount {
				params.count = countVal
			} else if countVal > maxItemCount {
				params.count = maxItemCount
			}
		}
	}

	// Parse tags
	if xt := getParam("xt"); xt != "" {
		params.excludeTags = strings.Split(xt, ",")
	}
	if it := getParam("it"); it != "" {
		params.includeTags = strings.Split(it, ",")
	}

	// Parse continuation
	params.continuation = getParam("c")

	// Parse time filters
	if ot := getParam("ot"); ot != "" {
		if ts, err := strconv.ParseInt(ot, 10, 64); err == nil {
			t := time.Unix(ts, 0)
			params.olderThan = &t
		}
	}
	if nt := getParam("nt"); nt != "" {
		if ts, err := strconv.ParseInt(nt, 10, 64); err == nil {
			t := time.Unix(ts, 0)
			params.newerThan = &t
		}
	}

	// Parse ordering
	params.oldestFirst = getParam("r") == "o"

	return params
}

// configureItemFilter creates and configures an ItemFilter based on stream type and parameters
func configureItemFilter(s *Server, stream Stream, params streamParams, userID int64) storage.ItemFilter {
	filter := storage.ItemFilter{}

	// Configure based on stream type
	switch stream.Type {
	case ReadingListStream:
		// Reading list generally implies unread items unless explicitly filtered otherwise
		status := storage.UNREAD
		filter.Status = &status
	case StarredStream:
		status := storage.STARRED
		filter.Status = &status
	case ReadStream:
		// Read stream often implies "all items"
	case FeedStream:
		if feedID, err := strconv.ParseInt(stream.ID, 10, 64); err == nil {
			filter.FeedID = &feedID
		}
	case LabelStream:
		// Find folder by title
		folders := s.db.ListFolders()
		for _, folder := range folders {
			if folder.Title == stream.ID {
				filter.FolderID = &folder.Id
				break
			}
		}
	}

	// Apply exclude tags filter
	for _, tag := range params.excludeTags {
		if strings.HasSuffix(tag, "/read") {
			status := storage.UNREAD
			filter.Status = &status
		}
	}

	// Apply include tags filter
	for _, tag := range params.includeTags {
		if strings.HasSuffix(tag, "/read") {
			status := storage.READ
			filter.Status = &status
		}
	}

	// Apply continuation based on sort order
	if params.continuation != "" {
		if contID, err := strconv.ParseInt(params.continuation, 10, 64); err == nil {
			if !params.oldestFirst {
				// Newest first: continuation ID is the oldest item from previous batch.
				// We need items *before* (older than/smaller ID than) this ID.
				filter.MaxID = &contID
			} else {
				// Oldest first: continuation ID is the newest item from previous batch.
				// We need items *after* (newer than/larger ID than) this ID.
				filter.SinceID = &contID
			}
		} else {
			log.Printf("[GReader Filter] Error parsing continuation ID '%s': %v", params.continuation, err)
		}
	}

	// Apply time filters ONLY if continuation ID was NOT provided (or was invalid)
	if params.continuation == "" || filter.MaxID == nil && filter.SinceID == nil {
		if params.olderThan != nil {
			filter.Before = params.olderThan
		}
		if params.newerThan != nil {
			filter.After = new(int64)
			*filter.After = params.newerThan.Unix()
		}
	}

	// Log the final filter details
	log.Printf("[GReader Filter] Final filter for stream %v with params %+v: %+v", stream, params, filter)

	return filter
}

// Handler for ClientLogin endpoint
func (s *Server) handleClientLogin(c *router.Context) {
	log.Printf("[GReader] Login attempt from %s", c.Req.RemoteAddr)

	var username, password string

	switch c.Req.Method {
	case http.MethodGet:
		log.Printf("[GReader] Handling GET request")
		username = c.Req.URL.Query().Get("Email")
		password = c.Req.URL.Query().Get("Passwd")
	case http.MethodPost:
		log.Printf("[GReader] Handling POST request")
		contentType := c.Req.Header.Get("Content-Type")
		log.Printf("[GReader] Content-Type: %s", contentType)
		if !strings.Contains(contentType, "application/x-www-form-urlencoded") {
			log.Printf("[GReader] Invalid content type %s from %s - expected application/x-www-form-urlencoded",
				contentType, c.Req.RemoteAddr)
		}

		if err := c.Req.ParseForm(); err != nil {
			log.Printf("[GReader] Failed to parse form data from %s: %v", c.Req.RemoteAddr, err)
			c.JSON(http.StatusBadRequest, map[string]interface{}{
				"error": "Invalid form data",
			})
			return
		}
		log.Printf("[GReader] POST form data: %v", c.Req.PostForm)
		username = c.Req.FormValue("Email")
		password = c.Req.FormValue("Passwd")
	default:
		log.Printf("[GReader] Unsupported method %s from %s", c.Req.Method, c.Req.RemoteAddr)
		c.Out.Header().Set("Allow", "GET, POST")
		c.JSON(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "Method Not Allowed",
		})
		return
	}

	// Get output parameter from form or query
	output := c.Req.FormValue("output")
	if output == "" {
		output = c.Req.URL.Query().Get("output")
	}

	log.Printf("[GReader] Login attempt for user %s from %s (output: %s)", username, c.Req.RemoteAddr, output)

	if username == "" || password == "" {
		log.Printf("[GReader] Missing credentials from %s - username empty: %v, password empty: %v",
			c.Req.RemoteAddr, username == "", password == "")
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Missing credentials - Email and Passwd parameters are required",
		})
		return
	}

	// In this implementation, we'll check against the server's configured username/password
	if s.Username == "" || s.Password == "" ||
		!auth.StringsEqual(username, s.Username) ||
		!auth.StringsEqual(password, s.Password) {
		log.Printf("[GReader] Invalid credentials from %s - server username empty: %v, server password empty: %v",
			c.Req.RemoteAddr, s.Username == "", s.Password == "")
		c.JSON(http.StatusUnauthorized, map[string]interface{}{
			"error": "Invalid credentials",
		})
		return
	}

	token := getAuthToken(username, password)
	result := login{SID: token, LSID: token, Auth: token}

	log.Printf("[GReader] Successful login for user %s from %s", username, c.Req.RemoteAddr)

	if strings.EqualFold(output, "json") {
		c.JSON(http.StatusOK, result)
		return
	}

	c.Out.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	c.Out.WriteHeader(http.StatusOK)
	c.Out.Write([]byte(result.String()))
}

// Handler for token endpoint
func (s *Server) handleToken(c *router.Context) {
	// Generate a unique session token by combining auth token with timestamp
	authToken := getAuthToken(s.Username, s.Password)
	sessionToken := fmt.Sprintf("%s%d", authToken, time.Now().UnixNano())
	paddedToken := fmt.Sprintf("%-24s", sessionToken)[:24] // Pad to 24 chars

	c.Out.Header().Add("Content-Type", "text/plain; charset=UTF-8")
	c.Out.WriteHeader(http.StatusOK)
	c.Out.Write([]byte(paddedToken))
}

// Helper function to write response in requested format
func writeResponse(c *router.Context, data interface{}) {
	output := c.Req.URL.Query().Get("output")
	if output == "xml" {
		// Since we don't have XML support in router.Context, return JSON
		c.JSON(http.StatusOK, data)
	} else {
		c.JSON(http.StatusOK, data)
	}
}

// Handler for user-info endpoint
func (s *Server) handleUserInfo(c *router.Context) {
	writeResponse(c, map[string]interface{}{
		"userInfo": userInfo{
			UserID:        "1",
			UserName:      s.Username,
			UserProfileID: "1",
			UserEmail:     s.Username,
		},
	})
}

// Handler for tag/list endpoint
func (s *Server) handleTagList(c *router.Context) {
	folders := s.db.ListFolders()
	tags := make([]subscriptionCategory, 0)

	// Add all standard system tags
	systemTags := []string{
		Read,
		Starred,
		ReadingList,
		KeptUnread,
		Fresh,
		All,
		Like,
		Broadcast,
		RecommendedSources,
		BlogSearch,
		PopularItems,
		Comments,
		Created,
		Shared,
		Notes,
	}

	// Add system tags with sortids
	for i, tag := range systemTags {
		sortID := fmt.Sprintf("A%07d", i+1)
		tags = append(tags, subscriptionCategory{
			ID:     fmt.Sprintf(UserStreamPrefix, 1) + tag,
			SortID: sortID,
		})
	}

	// Add folders as labels with continuing sortids
	for i, folder := range folders {
		sortID := fmt.Sprintf("A%07d", len(systemTags)+i+1)
		tags = append(tags, subscriptionCategory{
			ID:     fmt.Sprintf(UserLabelPrefix, 1) + folder.Title,
			SortID: sortID,
			Label:  folder.Title,
			Type:   "folder",
		})
	}

	writeResponse(c, tagsResponse{
		Tags: tags,
	})
}

// Handler for subscription/list endpoint
func (s *Server) handleSubscriptionList(c *router.Context) {
	feeds := s.db.ListFeeds()
	subsList := make([]subscription, 0)

	// Create a map of folder IDs to titles for efficient lookup
	folders := s.db.ListFolders()
	folderMap := make(map[int64]string, len(folders))
	for _, folder := range folders {
		folderMap[folder.Id] = folder.Title
	}

	for _, feed := range feeds {
		categories := make([]subscriptionCategory, 0)
		if feed.FolderId != nil {
			// Use the folder map for quick lookup
			if folderTitle, exists := folderMap[*feed.FolderId]; exists {
				categories = append(categories, subscriptionCategory{
					ID:    fmt.Sprintf(UserLabelPrefix, 1) + folderTitle,
					Label: folderTitle,
					Type:  "folder",
				})
			}
		}

		var iconURL string = ""
		if feed.HasIcon {
			iconURL = fmt.Sprintf("/fever/icon/%d", feed.Id)
		}

		subsList = append(subsList, subscription{
			ID:         fmt.Sprintf(FeedPrefix+"%d", feed.Id),
			Title:      feed.Title,
			URL:        feed.FeedLink,
			HTMLURL:    feed.Link,
			Categories: categories,
			IconURL:    iconURL,
		})
	}

	writeResponse(c, subscriptionsResponse{
		Subscriptions: subsList,
	})
}

// Handler for stream/items/ids endpoint
func (s *Server) handleStreamItemIDs(c *router.Context) {
	log.Printf("[GReader StreamItemIDs] Request from %s for stream var: '%s'", c.Req.RemoteAddr, c.Vars["stream"])

	// Parse stream parameters
	params := parseStreamParams(c.Req)

	// Get stream ID from path variable if present
	streamID := c.Vars["stream"]
	if streamID == "" {
		// If no stream ID in path, default to reading-list
		log.Printf("[GReader StreamItemIDs] No stream ID in path var, defaulting to reading-list")
		streamID = userStreamID(defaultUserID, ReadingList)
	}

	stream, err := getStream(streamID, defaultUserID)
	if err != nil {
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": fmt.Sprintf("Invalid stream ID: '%s'", streamID),
		})
		return
	}

	// Configure item filter
	filter := configureItemFilter(s, stream, params, defaultUserID)

	// Get items with one extra to determine continuation
	items := s.db.ListItems(filter, params.count+1, !params.oldestFirst, false)

	// Determine continuation token
	var continuation string
	if len(items) > params.count {
		items = items[:params.count]
		// Revert to using Item ID for continuation for now
		if len(items) > 0 {
			continuation = fmt.Sprintf("%d", items[len(items)-1].Id)
		}
	}

	// Build response
	response := map[string]interface{}{
		"itemRefs": make([]map[string]interface{}, len(items)),
	}
	if continuation != "" {
		response["continuation"] = continuation
	}

	for i, item := range items {
		response["itemRefs"].([]map[string]interface{})[i] = map[string]interface{}{
			"id": fmt.Sprintf(EntryIDLong, item.Id),
			"directStreamIds": []string{
				// TODO: Should this be the actual stream ID?
				fmt.Sprintf("user/%d/state/com.google/reading-list", defaultUserID),
			},
		}
	}

	c.JSON(http.StatusOK, response)
}

// Handler for stream/items/contents endpoint
func (s *Server) handleStreamItemContents(c *router.Context) {
	log.Printf("[GReader StreamItemContents] Request from %s", c.Req.RemoteAddr)

	// Parse form data if it's a POST request
	if c.Req.Method == http.MethodPost {
		if err := c.Req.ParseForm(); err != nil {
			c.JSON(http.StatusBadRequest, map[string]interface{}{
				"error": "Invalid form data",
			})
			return
		}
	}

	// Get item IDs from request (either query params or form data)
	itemIDs := c.Req.Form["i"]
	var items []storage.Item
	var streamID string
	var continuation string

	if len(itemIDs) > 0 {
		// Handle specific item IDs request
		log.Printf("[GReader StreamItemContents] Fetching specific items: %v", itemIDs)
		itemIDsInt := make([]int64, 0, len(itemIDs))
		for _, idStr := range itemIDs {
			var itemID int64
			_, err := fmt.Sscanf(idStr, EntryIDLong, &itemID)
			if err != nil {
				// Try short format
				_, err = fmt.Sscanf(idStr, EntryIDShort, &itemID)
				if err != nil {
					// Try plain number
					itemID, err = strconv.ParseInt(idStr, 10, 64)
					if err != nil {
						log.Printf("[GReader StreamItemContents] Skipping invalid item ID format: %s", idStr)
						continue
					}
				}
			}
			itemIDsInt = append(itemIDsInt, itemID)
		}
		filter := storage.ItemFilter{IDs: &itemIDsInt}
		items = s.db.ListItems(filter, len(itemIDsInt), true, true)
	} else {
		// Handle stream-based request
		params := parseStreamParams(c.Req)
		streamID = c.Vars["stream"]
		if streamID == "" {
			streamID = userStreamID(defaultUserID, ReadingList)
		}

		stream, err := getStream(streamID, defaultUserID)
		if err != nil {
			c.JSON(http.StatusBadRequest, map[string]interface{}{
				"error": "Invalid stream ID",
			})
			return
		}

		log.Printf("[GReader StreamItemContents] Fetching stream: %s", streamID)
		filter := configureItemFilter(s, stream, params, defaultUserID)
		items = s.db.ListItems(filter, params.count+1, !params.oldestFirst, true)

		// Handle continuation
		if len(items) > params.count {
			items = items[:params.count]
			// Use the last item's ID as the continuation token
			if len(items) > 0 {
				continuation = fmt.Sprintf("%d", items[len(items)-1].Id)
			}
		}
	}

	// Collect unique feed IDs
	feedIDs := make(map[int64]struct{})
	for _, item := range items {
		if item.Id <= 0 {
			continue
		}
		feedIDs[item.FeedId] = struct{}{}
	}

	// Batch fetch all required feeds
	feedMap := make(map[int64]*storage.Feed)
	if len(feedIDs) > 0 {
		feedIDList := make([]int64, 0, len(feedIDs))
		for id := range feedIDs {
			feedIDList = append(feedIDList, id)
		}
		feeds := s.db.GetFeeds(feedIDList)
		for _, feed := range feeds {
			feedMap[feed.Id] = feed
		}
	}

	// Build response
	response := map[string]interface{}{
		"id":      streamID,
		"updated": time.Now().Unix(),
		"items":   make([]map[string]interface{}, len(items)),
	}

	// Add continuation token if present
	if continuation != "" {
		response["continuation"] = continuation
	}

	for i, item := range items {
		if item.Id <= 0 {
			log.Printf("[GReader StreamItemContents] Warning: Item with invalid ID found, skipping")
			continue
		}

		// Get feed info from the map
		feed, exists := feedMap[item.FeedId]
		if !exists {
			log.Printf("[GReader StreamItemContents] Warning: Feed %d not found for item %d", item.FeedId, item.Id)
			continue
		}

		// Build categories array
		categories := []string{
			fmt.Sprintf("user/%d/state/com.google/reading-list", defaultUserID),
		}
		if item.Status == storage.READ {
			categories = append(categories, fmt.Sprintf("user/%d/state/com.google/read", defaultUserID))
		}
		if item.Status == storage.STARRED {
			categories = append(categories, fmt.Sprintf("user/%d/state/com.google/starred", defaultUserID))
		}

		itemMap := map[string]interface{}{
			"id":            fmt.Sprintf(EntryIDLong, item.Id),
			"title":         item.Title,
			"published":     item.Date.Unix(),
			"crawlTimeMsec": fmt.Sprintf("%d", item.Date.UnixNano()/int64(time.Millisecond)),
			"timestampUsec": fmt.Sprintf("%d", item.Date.UnixNano()/int64(time.Microsecond)),
			"summary": map[string]interface{}{
				"content": item.Content,
			},
			"alternate": []map[string]interface{}{
				{
					"href": item.Link,
				},
			},
			"canonical": []map[string]interface{}{
				{
					"href": item.Link,
				},
			},
			"categories": categories,
			"origin": map[string]interface{}{
				"streamId": fmt.Sprintf("feed/%d", feed.Id),
				"title":    feed.Title,
				"htmlUrl":  feed.Link,
			},
		}

		response["items"].([]map[string]interface{})[i] = itemMap
	}

	c.JSON(http.StatusOK, response)
}

// Handler for edit-tag endpoint (mark items as read/unread/starred)
func (s *Server) handleEditTag(c *router.Context) {
	log.Printf("[GReader EditTag] Debug: Request method: %s", c.Req.Method)
	log.Printf("[GReader EditTag] Debug: Request URL: %s", c.Req.URL.String())
	log.Printf("[GReader EditTag] Debug: Request headers: %v", c.Req.Header)
	log.Printf("[GReader EditTag] Debug: Raw query: %s", c.Req.URL.RawQuery)

	// Enforce POST method as per spec
	if c.Req.Method != http.MethodPost {
		log.Printf("[GReader EditTag] Received non-POST request (%s) from %s", c.Req.Method, c.Req.RemoteAddr)
		c.Out.Header().Set("Allow", http.MethodPost)
		c.JSON(http.StatusMethodNotAllowed, map[string]interface{}{
			"error": "Method Not Allowed: Use POST for edit-tag",
		})
		return
	}

	if err := c.Req.ParseForm(); err != nil {
		log.Printf("[GReader EditTag] Failed to parse form data: %v", err)
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid form data",
		})
		return
	}

	// Log all form values
	log.Printf("[GReader EditTag] Debug: Form values: %v", c.Req.Form)
	log.Printf("[GReader EditTag] Debug: Query values: %v", c.Req.URL.Query())

	// Get item IDs
	itemIDsParam := c.Req.Form["i"]
	if len(itemIDsParam) == 0 {
		log.Printf("[GReader EditTag] No items specified in request")
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "No items specified ('i' parameter)",
		})
		return
	}

	log.Printf("[GReader EditTag] Processing item IDs: %v", itemIDsParam)

	itemIDs := make([]int64, 0, len(itemIDsParam))
	for _, idStr := range itemIDsParam {
		// Google Reader format uses hex/long IDs, adapt to our system
		var itemID int64
		var err error

		// First try to parse as a plain number
		itemID, err = strconv.ParseInt(idStr, 10, 64)
		if err == nil {
			log.Printf("[GReader EditTag] Successfully parsed plain number ID: %d", itemID)
			itemIDs = append(itemIDs, itemID)
			continue
		}

		// Try Google Reader long format
		_, err = fmt.Sscanf(idStr, EntryIDLong, &itemID)
		if err == nil {
			log.Printf("[GReader EditTag] Successfully parsed long format ID: %d", itemID)
			itemIDs = append(itemIDs, itemID)
			continue
		}

		// Try short format
		_, err = fmt.Sscanf(idStr, EntryIDShort, &itemID)
		if err == nil {
			log.Printf("[GReader EditTag] Successfully parsed short format ID: %d", itemID)
			itemIDs = append(itemIDs, itemID)
			continue
		}

		log.Printf("[GReader EditTag] Skipping invalid item ID format: %s", idStr)
	}

	if len(itemIDs) == 0 {
		log.Printf("[GReader EditTag] No valid item IDs found after parsing")
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "No valid item IDs found",
		})
		return
	}

	// Get tags to add and remove
	addTags := c.Req.Form["a"]    // Tags to add
	removeTags := c.Req.Form["r"] // Tags to remove

	log.Printf("[GReader EditTag] Debug: Add tags: %v", addTags)
	log.Printf("[GReader EditTag] Debug: Remove tags: %v", removeTags)

	if len(addTags) == 0 && len(removeTags) == 0 {
		log.Printf("[GReader EditTag] No tags specified for add or remove")
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "No tags specified for add ('a') or remove ('r')",
		})
		return
	}

	log.Printf("[GReader EditTag] Processing tags - Add: %v, Remove: %v", addTags, removeTags)

	// Process tags
	// Note: GReader allows multiple tags, but we mainly care about read/starred states
	for _, tag := range addTags {
		stream, err := getStream(tag, defaultUserID)
		if err != nil {
			log.Printf("[GReader EditTag] Skipping invalid add tag: %s", tag)
			continue
		}

		switch stream.Type {
		case ReadStream:
			// Mark items as read
			log.Printf("[GReader EditTag] Marking items %v as READ", itemIDs)
			s.db.UpdateMultipleItemStatuses(itemIDs, storage.READ)
		case StarredStream:
			// Mark items as starred
			log.Printf("[GReader EditTag] Marking items %v as STARRED", itemIDs)
			s.db.UpdateMultipleItemStatuses(itemIDs, storage.STARRED)
		default:
			log.Printf("[GReader EditTag] Skipping unsupported add tag type: %s", tag)
		}
	}

	for _, tag := range removeTags {
		stream, err := getStream(tag, defaultUserID)
		if err != nil {
			log.Printf("[GReader EditTag] Skipping invalid remove tag: %s", tag)
			continue
		}

		switch stream.Type {
		case ReadStream:
			// Mark items as unread (remove the read state)
			log.Printf("[GReader EditTag] Marking items %v as UNREAD", itemIDs)
			s.db.UpdateMultipleItemStatuses(itemIDs, storage.UNREAD)
		case StarredStream:
			// Mark items as unstarred (remove the starred state) -> goes back to UNREAD usually
			// Check current status before deciding? GReader might implicitly set to read.
			// Let's set to UNREAD for simplicity unless already read.
			log.Printf("[GReader EditTag] Marking items %v as UNSTARRED (-> UNREAD)", itemIDs)
			s.db.UpdateMultipleItemStatuses(itemIDs, storage.UNREAD)
		default:
			log.Printf("[GReader EditTag] Skipping unsupported remove tag type: %s", tag)
		}
	}

	c.Out.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	c.Out.WriteHeader(http.StatusOK)
	c.Out.Write([]byte("OK"))
}

// Handler for unread-count endpoint
func (s *Server) handleUnreadCount(c *router.Context) {
	feeds := s.db.ListFeeds()
	counts := make([]unreadCount, 0)
	totalUnread := 0
	maxTimestamp := int64(0) // Overall newest item timestamp

	// Get unread count for reading list (all unread items)
	filter := storage.ItemFilter{}
	status := storage.UNREAD
	filter.Status = &status
	readingListCount := s.db.CountItems(filter)
	var newestReadingListItemTimestamp int64
	if readingListCount > 0 {
		items := s.db.ListItems(filter, 1, true, false)
		if len(items) > 0 {
			newestReadingListItemTimestamp = items[0].Date.UnixNano() / 1000 // Microseconds
			if newestReadingListItemTimestamp > maxTimestamp {
				maxTimestamp = newestReadingListItemTimestamp
			}
		}
		counts = append(counts, unreadCount{
			ID:         fmt.Sprintf(UserStreamPrefix, 1) + ReadingList,
			Count:      readingListCount,
			NewestItem: newestReadingListItemTimestamp,
		})
		totalUnread += readingListCount
	}

	// Get unread count for each feed
	for _, feed := range feeds {
		feedFilter := storage.ItemFilter{
			FeedID: &feed.Id,
			Status: &status,
		}
		count := s.db.CountItems(feedFilter)
		var newestFeedItemTimestamp int64
		if count > 0 {
			// Get newest item timestamp for this feed
			items := s.db.ListItems(feedFilter, 1, true, false)
			if len(items) > 0 {
				newestFeedItemTimestamp = items[0].Date.UnixNano() / 1000 // Microseconds
				if newestFeedItemTimestamp > maxTimestamp {
					maxTimestamp = newestFeedItemTimestamp
				}
			}
			counts = append(counts, unreadCount{
				ID:         fmt.Sprintf(FeedPrefix+"%d", feed.Id),
				Count:      count,
				NewestItem: newestFeedItemTimestamp,
			})
			// Note: GReader spec sums feed counts into total, but our 'readingListCount' already covers all unread.
			// Avoid double counting. The spec is a bit ambiguous here. Let's stick to totalUnread from ReadingList.
		}
	}

	// Get unread count for each folder
	for _, folder := range s.db.ListFolders() {
		folderFilter := storage.ItemFilter{
			FolderID: &folder.Id,
			Status:   &status,
		}
		count := s.db.CountItems(folderFilter)
		var newestFolderItemTimestamp int64
		if count > 0 {
			items := s.db.ListItems(folderFilter, 1, true, false)
			if len(items) > 0 {
				newestFolderItemTimestamp = items[0].Date.UnixNano() / 1000 // Microseconds
				// No need to update maxTimestamp here, already covered by feed/reading list checks
			}
			counts = append(counts, unreadCount{
				ID:         fmt.Sprintf(UserLabelPrefix, 1) + folder.Title,
				Count:      count,
				NewestItem: newestFolderItemTimestamp,
			})
		}
	}

	// Add starred count (not strictly unread, but often included)
	statusStarred := storage.STARRED
	starredFilter := storage.ItemFilter{Status: &statusStarred}
	starredCount := s.db.CountItems(starredFilter)
	var newestStarredTimestamp int64
	if starredCount > 0 {
		items := s.db.ListItems(starredFilter, 1, true, false)
		if len(items) > 0 {
			newestStarredTimestamp = items[0].Date.UnixNano() / 1000
		}
		counts = append(counts, unreadCount{
			ID:         fmt.Sprintf(UserStreamPrefix, 1) + Starred,
			Count:      starredCount,
			NewestItem: newestStarredTimestamp,
		})
	}

	writeResponse(c, unreadCountResponse{
		Max: totalUnread, // GReader 'max' usually means total unread capacity, often set high or same as count
		// Unread field is deprecated/unused by many clients, but include for compatibility
		Counts: counts,
	})
}

// Handler for mark-all-as-read endpoint
func (s *Server) handleMarkAllAsRead(c *router.Context) {
	if err := c.Req.ParseForm(); err != nil {
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid form data",
		})
		return
	}

	streamID := c.Req.Form.Get("s")
	if streamID == "" {
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "No stream specified ('s' parameter)",
		})
		return
	}
	log.Printf("[GReader MarkRead] Request for stream: %s from %s", streamID, c.Req.RemoteAddr)

	// Parse timestamp (mark items older than this timestamp as read)
	var ts *time.Time
	// GReader uses 'ts' in microseconds string format for mark-all-as-read
	if tsStr := c.Req.Form.Get("ts"); tsStr != "" {
		if tsInt, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
			// Convert microseconds to time.Time
			t := time.Unix(0, tsInt*1000) // nano = micro * 1000
			ts = &t
			log.Printf("[GReader MarkRead] Timestamp filter (older than): %s from %s", ts.String(), c.Req.RemoteAddr)
		} else {
			log.Printf("[GReader MarkRead] Error parsing timestamp 'ts'=%s: %v", tsStr, err)
			// Don't fail, just ignore the timestamp if invalid
		}
	}

	stream, err := getStream(streamID, 1) // User ID 1
	if err != nil {
		log.Printf("[GReader MarkRead] Invalid stream ID: %s from %s: %v", streamID, c.Req.RemoteAddr, err)
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Invalid stream ID",
		})
		return
	}

	filter := storage.MarkFilter{} // Filter for marking items

	switch stream.Type {
	case ReadingListStream:
		// Mark all unread items as read (optionally older than ts)
		log.Printf("[GReader MarkRead] Marking all items in Reading List")
	case FeedStream:
		feedID, err := strconv.ParseInt(stream.ID, 10, 64)
		if err != nil {
			log.Printf("[GReader MarkRead] Invalid feed ID in stream: %s", stream.ID)
			c.JSON(http.StatusBadRequest, map[string]interface{}{
				"error": "Invalid feed ID",
			})
			return
		}
		filter.FeedID = &feedID
		log.Printf("[GReader MarkRead] Marking items in Feed ID: %d", feedID)
	case LabelStream:
		// Find folder by title
		folders := s.db.ListFolders()
		var folderID *int64
		for _, folder := range folders {
			if folder.Title == stream.ID {
				fID := folder.Id // Need to capture the value
				folderID = &fID
				break
			}
		}
		if folderID != nil {
			filter.FolderID = folderID
			log.Printf("[GReader MarkRead] Marking items in Folder ID: %d (Title: %s)", *folderID, stream.ID)
		} else {
			log.Printf("[GReader MarkRead] Folder not found for label: %s", stream.ID)
			c.JSON(http.StatusBadRequest, map[string]interface{}{
				"error": "Label (folder) not found",
			})
			return
		}
	default:
		log.Printf("[GReader MarkRead] Unsupported stream type for mark-all-as-read: %v", stream.Type)
		c.JSON(http.StatusBadRequest, map[string]interface{}{
			"error": "Unsupported stream type for this operation",
		})
		return
	}

	// Apply timestamp filter if provided
	if ts != nil {
		filter.Before = ts
	}

	s.db.MarkItemsRead(filter)
	log.Printf("[GReader MarkRead] MarkItemsRead called with filter: %+v", filter)

	c.Out.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	c.Out.WriteHeader(http.StatusOK)
	c.Out.Write([]byte("OK"))
}

// Handler for all Google Reader API endpoints
func (s *Server) handleGoogleReader(c *router.Context) {
	path := c.Req.URL.Path
	log.Printf("[GReader Handler] Entered for %s %s from %s", c.Req.Method, path, c.Req.RemoteAddr)

	// Authentication for all endpoints except ClientLogin
	if !strings.HasSuffix(path, "/accounts/ClientLogin") {
		// Check for valid token in Authorization header
		authHeader := c.Req.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(authHeader, "GoogleLogin auth=") {
			token = strings.TrimPrefix(authHeader, "GoogleLogin auth=")
		}

		// If not in header, check standard GReader locations: T param, auth param
		if token == "" {
			if tParam := c.Req.FormValue("T"); tParam != "" { // Check form first (POST or GET after ParseForm)
				token = tParam
			} else if authParam := c.Req.URL.Query().Get("auth"); authParam != "" { // Check query param
				token = authParam
			} else if authParamAlt := c.Req.URL.Query().Get("Authorization"); authParamAlt != "" { // Some clients might use this non-standard query param
				token = authParamAlt
			}
		}

		if token == "" {
			log.Printf("[GReader Auth] Unauthorized: No token found for %s from %s", path, c.Req.RemoteAddr)
			c.Out.WriteHeader(http.StatusUnauthorized)
			c.Out.Write([]byte("Unauthorized: Missing token"))
			return
		}

		expectedToken := getAuthToken(s.Username, s.Password)

		if !auth.StringsEqual(token, expectedToken) {
			log.Printf("[GReader Auth] Unauthorized: Invalid token provided for %s from %s", path, c.Req.RemoteAddr)
			c.Out.WriteHeader(http.StatusUnauthorized)
			c.Out.Write([]byte("Unauthorized: Invalid token"))
			return
		}
		// log.Printf("[GReader Auth] Authorized access to %s for user %s from %s", path, s.Username, c.Req.RemoteAddr)
	}

	// Route to the appropriate handler based on the endpoint suffix
	// Assumes base path is stripped by the time it gets here (e.g., "/reader/api/0")
	// Correctly find the path relative to the API base path
	apiBase := "/reader/api/0"
	clientLoginPath := "/accounts/ClientLogin" // Define the specific path
	var apiPath string
	// Check if the path ENDS with the ClientLogin path first
	if strings.HasSuffix(path, clientLoginPath) {
		apiPath = clientLoginPath // Use the canonical path for the switch case
	} else {
		// Otherwise, handle paths relative to the standard API base
		idx := strings.Index(path, apiBase)
		if idx != -1 {
			apiPath = path[idx+len(apiBase):]
		} else {
			log.Printf("[GReader Routing] Error: API base path '%s' not found in request path '%s' and not ClientLogin", apiBase, path)
			// Fall through to default handler for safety, maybe it's an unexpected path
			apiPath = path // Use full path for default case matching
		}
	}

	switch {
	case apiPath == clientLoginPath: // Match specific path (now works with prefixes)
		s.handleClientLogin(c)
	case apiPath == "/token":
		s.handleToken(c)
	case apiPath == "/user-info":
		s.handleUserInfo(c)
	case apiPath == "/tag/list":
		s.handleTagList(c)
	case apiPath == "/subscription/list":
		s.handleSubscriptionList(c)
	case strings.HasPrefix(apiPath, "/stream/items/ids"): // Match base and wildcard route (/ids or /ids/...)
		s.handleStreamItemIDs(c)
	case strings.HasPrefix(apiPath, "/stream/items/contents"), strings.HasPrefix(apiPath, "/stream/contents/"): // Match both content endpoints
		s.handleStreamItemContents(c)
	case apiPath == "/edit-tag":
		s.handleEditTag(c)
	case apiPath == "/unread-count":
		s.handleUnreadCount(c)
	case apiPath == "/mark-all-as-read":
		s.handleMarkAllAsRead(c)
	// Add handlers for other endpoints like quickadd, preference/list, etc. if needed
	default:
		// Default handler for unimplemented endpoints - return OK to avoid client errors
		log.Printf("[GReader] Unimplemented Google Reader API endpoint request: %s (apiPath: %s) from %s", path, apiPath, c.Req.RemoteAddr)
		// Returning empty OK for compatibility, some clients might poll unsupported endpoints
		// c.JSON(http.StatusNotImplemented, map[string]interface{}{"error": "Not Implemented"})
		OK(c)
	}
}

// Register routes for Google Reader API
func (s *Server) RegisterGoogleReaderRoutes(r *router.Router) {
	// Define base paths - Use a group if router supports it, otherwise repeat prefix
	basePath := "/reader/api/0"

	// Specific endpoints using the single handler which routes internally
	// ClientLogin is outside the standard API path
	r.For("/accounts/ClientLogin", s.handleGoogleReader)

	// Standard API endpoints
	// Assuming default handler routes handle GET/POST as needed internally
	r.For(basePath+"/token", s.handleGoogleReader)
	r.For(basePath+"/user-info", s.handleGoogleReader)
	r.For(basePath+"/tag/list", s.handleGoogleReader)
	r.For(basePath+"/subscription/list", s.handleGoogleReader)
	// Keep only the wildcard route, router should handle matching both cases
	r.For(basePath+"/stream/items/ids/*stream", s.handleGoogleReader) // Route with wildcard
	r.For(basePath+"/stream/items/contents", s.handleGoogleReader)
	r.For(basePath+"/stream/contents/*streamId", s.handleGoogleReader)
	r.For(basePath+"/edit-tag", s.handleGoogleReader)
	r.For(basePath+"/unread-count", s.handleGoogleReader)
	r.For(basePath+"/mark-all-as-read", s.handleGoogleReader)

	// Optional: Add fallbacks for paths without /0/ if needed, though clients should use /0/
	// r.For("/reader/api/token", s.handleGoogleReader).Methods("GET")

	log.Println("[Server] Google Reader API routes registered")
}
