import os

import google_reader
import requests

YARR_URL = os.environ.get("YARR_URL", "http://localhost:7070")


def test_mark_as_read(client, auth, csrf_token, item_ids):
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)

    # Mark as read
    client.edit_tags(
        auth, csrf_token,
        item_ids=[long_id],
        add_tags=[google_reader.STREAM_READ],
    )

    # Verify it's in the read state (excluded from unread)
    unread = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        exclude_target=google_reader.STREAM_READ,
        limit=100,
    )
    unread_ids = [r.id for r in unread.item_refs]
    assert item_ids[0] not in unread_ids


def test_mark_as_unread(client, auth, csrf_token, item_ids):
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)

    # Mark as unread
    client.edit_tags(
        auth, csrf_token,
        item_ids=[long_id],
        remove_tags=[google_reader.STREAM_READ],
    )

    # Verify it's back in the unread stream
    unread = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        exclude_target=google_reader.STREAM_READ,
        limit=100,
    )
    unread_ids = [r.id for r in unread.item_refs]
    assert item_ids[0] in unread_ids


def test_star_item(client, auth, csrf_token, item_ids):
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)

    # Star the item
    client.edit_tags(
        auth, csrf_token,
        item_ids=[long_id],
        add_tags=[google_reader.STREAM_STARRED],
    )

    # Verify it's in the starred stream
    starred = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_STARRED,
        limit=100,
    )
    starred_ids = [r.id for r in starred.item_refs]
    assert item_ids[0] in starred_ids


def test_unstar_item(client, auth, csrf_token, item_ids):
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)

    # Unstar the item
    client.edit_tags(
        auth, csrf_token,
        item_ids=[long_id],
        remove_tags=[google_reader.STREAM_STARRED],
    )

    # Verify it's no longer in the starred stream
    starred = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_STARRED,
        limit=100,
    )
    starred_ids = [r.id for r in starred.item_refs]
    assert item_ids[0] not in starred_ids


def test_batch_mark_as_read(client, auth, csrf_token, item_ids):
    """Mark multiple items as read in one request."""
    long_ids = [google_reader.get_long_item_id(int(id_)) for id_ in item_ids[:3]]

    client.edit_tags(
        auth, csrf_token,
        item_ids=long_ids,
        add_tags=[google_reader.STREAM_READ],
    )

    unread = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        exclude_target=google_reader.STREAM_READ,
        limit=100,
    )
    unread_ids = [r.id for r in unread.item_refs]
    for id_ in item_ids[:3]:
        assert id_ not in unread_ids

    # Undo: mark back as unread
    client.edit_tags(
        auth, csrf_token,
        item_ids=long_ids,
        remove_tags=[google_reader.STREAM_READ],
    )


def test_edit_tag_without_token():
    """Edit-tag without action token should fail."""
    resp = requests.post(
        f"{YARR_URL}/reader/api/0/edit-tag",
        headers={"Authorization": "GoogleLogin auth=invalid"},
    )
    assert resp.status_code == 401


def test_mark_all_as_read_for_feed(client, auth, csrf_token, seeded_feed):
    """Mark all items in a feed as read."""
    # Get the feed's stream ID
    subs = client.list_subscriptions(auth)
    feed = next(s for s in subs if s.title == "Test Feed")

    client.mark_all_as_read(auth, csrf_token, feed.id)

    # All items in feed should now be read
    unread = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        exclude_target=google_reader.STREAM_READ,
        limit=100,
    )
    assert len(unread.item_refs) == 0

    # Undo: mark all back as unread
    all_ids = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        limit=100,
    )
    long_ids = [google_reader.get_long_item_id(int(r.id)) for r in all_ids.item_refs]
    if long_ids:
        client.edit_tags(
            auth, csrf_token,
            item_ids=long_ids,
            remove_tags=[google_reader.STREAM_READ],
        )
