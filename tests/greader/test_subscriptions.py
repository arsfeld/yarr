import os
import time

import google_reader

TESTFEED_URL = os.environ.get("TESTFEED_URL", "http://testfeed")


def test_quickadd_subscribe(client, auth, csrf_token):
    """Subscribe to a new feed via quickadd."""
    feed_url = f"{TESTFEED_URL}/test-feed-2.xml"
    result = client.quick_add_subscription(auth, csrf_token, feed_url)
    assert result.stream_id.startswith("feed/")

    # Verify it appears in the subscription list
    subs = client.list_subscriptions(auth)
    titles = [s.title for s in subs]
    assert "Second Test Feed" in titles


def test_subscription_rename(client, auth, csrf_token):
    """Rename a feed via subscription/edit."""
    subs = client.list_subscriptions(auth)
    feed = next(s for s in subs if s.title == "Second Test Feed")

    client.edit_subscription(
        auth, csrf_token,
        subscription_id=feed.id,
        action="edit",
        title="Renamed Feed",
    )

    subs = client.list_subscriptions(auth)
    titles = [s.title for s in subs]
    assert "Renamed Feed" in titles
    assert "Second Test Feed" not in titles


def test_subscription_move_to_folder(client, auth, csrf_token):
    """Move a feed to a folder (creates the folder implicitly)."""
    subs = client.list_subscriptions(auth)
    feed = next(s for s in subs if s.title == "Renamed Feed")

    label = google_reader.get_label_id("Test Folder")
    client.edit_subscription(
        auth, csrf_token,
        subscription_id=feed.id,
        action="edit",
        add_label_id=label,
    )

    # Verify folder appears in tag list
    tags = client.list_tags(auth)
    tag_ids = [t.id for t in tags]
    assert label in tag_ids

    # Verify feed is in the folder
    subs = client.list_subscriptions(auth)
    feed = next(s for s in subs if s.title == "Renamed Feed")
    assert any(c.label == "Test Folder" for c in feed.categories)


def test_subscription_unsubscribe(client, auth, csrf_token):
    """Unsubscribe from a feed."""
    subs = client.list_subscriptions(auth)
    feed = next(s for s in subs if s.title == "Renamed Feed")

    client.edit_subscription(
        auth, csrf_token,
        subscription_id=feed.id,
        action="unsubscribe",
    )

    subs = client.list_subscriptions(auth)
    titles = [s.title for s in subs]
    assert "Renamed Feed" not in titles


def test_disable_tag_deletes_folder(client, auth, csrf_token):
    """Delete a folder via disable-tag."""
    label = google_reader.get_label_id("Test Folder")
    client.disable_tag(auth, csrf_token, label)

    tags = client.list_tags(auth)
    tag_ids = [t.id for t in tags]
    assert label not in tag_ids
