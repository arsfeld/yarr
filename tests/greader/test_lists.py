import os

import google_reader
import requests

YARR_URL = os.environ.get("YARR_URL", "http://localhost:7070")


def test_tag_list_contains_starred(client, auth, seeded_feed):
    tags = client.list_tags(auth)
    tag_ids = [t.id for t in tags]
    assert google_reader.STREAM_STARRED in tag_ids


def test_subscription_list_contains_feed(client, auth, seeded_feed):
    subs = client.list_subscriptions(auth)
    assert len(subs) >= 1
    titles = [s.title for s in subs]
    assert "Test Feed" in titles


def test_subscription_has_correct_fields(client, auth, seeded_feed):
    subs = client.list_subscriptions(auth)
    feed = next(s for s in subs if s.title == "Test Feed")
    assert feed.id.startswith("feed/")
    assert feed.url  # feed_link
    assert feed.html_url is not None  # site link


def test_unread_count(raw_session, seeded_feed):
    resp = raw_session.get(f"{YARR_URL}/reader/api/0/unread-count", params={"output": "json"})
    assert resp.status_code == 200
    data = resp.json()
    assert "unreadcounts" in data
    # Should have at least one feed with unread items
    total = sum(c["count"] for c in data["unreadcounts"])
    assert total >= 5  # Our test feed has 5 items, all initially unread
