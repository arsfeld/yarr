# Google Reader API support

Google Reader API is a widely used RSS HTTP API interface that was originally developed by Google for their Google Reader service. While the original service has been discontinued, the API remains popular and is supported by many RSS clients.

## Documentation

For more detailed information about the Google Reader API implementation, you can refer to these resources:

- [Re-Implementing the Google Reader API in 2025](https://www.davd.io/posts/2025-02-05-reimplementing-google-reader-api-in-2025/) - A comprehensive guide to implementing the Google Reader API
- [Google Reader API Documentation](https://github.com/theoldreader/api) - Unofficial API documentation
- [FreshRSS Google Reader API](https://github.com/FreshRSS/FreshRSS/blob/latest/p/api/greader.php) - FreshRSS implementation of the Google Reader API

The Google Reader API implemented by Yarr is based on the Google Reader API specification and is designed to be compatible with modern RSS clients.

> **Note:** The most popular reimplementation of the Google Reader API is FreshRSS. Many RSS clients expose this API option as "FreshRSS" rather than "Google Reader". When configuring clients, you may need to select "FreshRSS" as the service type even though you're using Yarr's Google Reader API implementation.

Here are some Apps that have been tested to work with yarr. Feel free to test other Clients/Apps and update the list here.

> Different apps support different URL/Address formats. Please note whether the URL entered has `http://` scheme and `/` suffix.

| App                                                                       | Platforms        | Config Server URL                                   |
|:------------------------------------------------------------------------- | ---------------- |:--------------------------------------------------- |
| [Newsflash](https://gitlab.com/news-flash/news_flash_gtk)                 | Linux            | http://127.0.0.1:7070/greader                 |
| [NetNewsWire](https://netnewswire.com/)                                   | MacOS<br>iOS     | http://127.0.0.1:7070/greader                 |

## API Endpoints

The following Google Reader API endpoints are supported:

- `/accounts/ClientLogin` - Authentication endpoint
- `/token` - Session token generation
- `/user-info` - User information
- `/tag/list` - List of tags/categories
- `/subscription/list` - List of subscribed feeds
- `/stream/items/ids` - Get item IDs for a stream
- `/stream/items/contents` - Get item contents
- `/edit-tag` - Mark items as read/unread/starred
- `/unread-count` - Get unread counts
- `/mark-all-as-read` - Mark all items in a stream as read

## Authentication

The Google Reader API uses token-based authentication. The authentication flow is:

1. Client calls `/accounts/ClientLogin` with username and password
2. Server returns authentication tokens (SID, LSID, Auth)
3. Client uses the Auth token in subsequent requests via:
   - Authorization header: `GoogleLogin auth=<token>`
   - URL parameter: `?auth=<token>`
   - Form parameter: `T=<token>`

## Stream Types

The following stream types are supported:

- Reading List (all unread items)
- Starred items
- Feed-specific streams
- Label/Folder streams

If you are having trouble using the Google Reader API, please open an issue and @arsfeld, thanks. 