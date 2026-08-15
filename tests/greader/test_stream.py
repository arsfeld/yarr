import google_reader


def test_stream_ids_reading_list(client, auth, seeded_feed):
    ids = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        limit=100,
    )
    assert len(ids.item_refs) == 5


def test_stream_ids_returns_decimal_ids(client, auth, seeded_feed):
    ids = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        limit=100,
    )
    for ref in ids.item_refs:
        # IDs should be decimal strings
        assert ref.id.isdigit()


def test_stream_ids_exclude_read(client, auth, csrf_token, item_ids):
    """After marking an item as read, it should be excluded with xt=read."""
    # Mark first item as read
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)
    client.edit_tags(
        auth, csrf_token,
        item_ids=[long_id],
        add_tags=[google_reader.STREAM_READ],
    )

    # Fetch unread items (reading-list minus read)
    ids = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        exclude_target=google_reader.STREAM_READ,
        limit=100,
    )
    returned_ids = [ref.id for ref in ids.item_refs]
    assert item_ids[0] not in returned_ids

    # Undo: mark back as unread
    client.edit_tags(
        auth, csrf_token,
        item_ids=[long_id],
        remove_tags=[google_reader.STREAM_READ],
    )


def test_stream_ids_starred_initially_empty(client, auth, seeded_feed):
    ids = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_STARRED,
        limit=100,
    )
    assert len(ids.item_refs) == 0


def test_stream_ids_pagination(client, auth, seeded_feed):
    """Request 2 items, expect continuation, then fetch more."""
    page1 = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        limit=2,
    )
    assert len(page1.item_refs) == 2
    assert page1.continuation

    page2 = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        limit=2,
        continuation=page1.continuation,
    )
    assert len(page2.item_refs) >= 1
    # Pages should have different items
    page1_ids = {r.id for r in page1.item_refs}
    page2_ids = {r.id for r in page2.item_refs}
    assert page1_ids.isdisjoint(page2_ids)


def test_stream_contents(client, auth, csrf_token, item_ids):
    """Fetch full content for items by ID."""
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)

    contents = client.get_stream_items_contents(auth, csrf_token, [long_id])
    assert len(contents.items) == 1

    item = contents.items[0]
    assert item.id.startswith("tag:google.com,2005:reader/item/")
    assert item.title
    assert item.published > 0
    assert item.origin
    assert item.origin.stream_id.startswith("feed/")


def test_stream_contents_has_summary(client, auth, csrf_token, item_ids):
    first_id = int(item_ids[0])
    long_id = google_reader.get_long_item_id(first_id)

    contents = client.get_stream_items_contents(auth, csrf_token, [long_id])
    item = contents.items[0]
    assert item.summary
    assert item.summary.content  # HTML content should be present


def test_stream_contents_nonexistent_ids(client, auth, csrf_token):
    """Non-existent IDs should return empty items, not error."""
    fake_id = google_reader.get_long_item_id(999999999)
    contents = client.get_stream_items_contents(auth, csrf_token, [fake_id])
    assert len(contents.items) == 0
