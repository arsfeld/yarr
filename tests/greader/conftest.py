import os
import time

import google_reader
import pytest
import requests


YARR_URL = os.environ.get("YARR_URL", "http://localhost:7070")
USERNAME = os.environ.get("YARR_USERNAME", "test")
PASSWORD = os.environ.get("YARR_PASSWORD", "test")
TESTFEED_URL = os.environ.get("TESTFEED_URL", "http://testfeed")


@pytest.fixture(scope="session")
def client():
    return google_reader.Client(YARR_URL)


@pytest.fixture(scope="session")
def auth(client):
    return client.login(USERNAME, PASSWORD)


@pytest.fixture(scope="session")
def csrf_token(client, auth):
    return client.get_token(auth)


@pytest.fixture(scope="session")
def raw_session():
    """A raw requests session with auth header for endpoints not in the library."""
    s = requests.Session()
    # Login to get the auth token
    resp = s.post(
        f"{YARR_URL}/accounts/ClientLogin",
        data={"Email": USERNAME, "Passwd": PASSWORD},
    )
    token = None
    for line in resp.text.strip().split("\n"):
        if line.startswith("Auth="):
            token = line.split("=", 1)[1]
    s.headers["Authorization"] = f"GoogleLogin auth={token}"
    return s


@pytest.fixture(scope="session")
def seeded_feed(client, auth, csrf_token):
    """Subscribe to the test feed and wait for items to be fetched."""
    feed_url = f"{TESTFEED_URL}/test-feed.xml"
    result = client.quick_add_subscription(auth, csrf_token, feed_url)

    # Wait briefly for yarr to fetch the feed items
    for _ in range(10):
        ids = client.get_stream_items_ids(
            auth,
            google_reader.STREAM_READING_LIST,
            limit=100,
        )
        if ids.item_refs:
            break
        time.sleep(0.5)

    return result


@pytest.fixture(scope="session")
def item_ids(client, auth, seeded_feed):
    """Get all item IDs from the seeded feed."""
    ids = client.get_stream_items_ids(
        auth,
        google_reader.STREAM_READING_LIST,
        limit=100,
    )
    return [ref.id for ref in ids.item_refs]
