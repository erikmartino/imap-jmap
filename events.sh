NC_HOST="cloud.profundo.dk"
NC_USER="erikmartino@profundo.dk"
NC_PASS="erikmartino@profundo.dk"

# Encode '@' to '%40' for the URL path
NC_USER_URL="${NC_USER//@/%40}"

# Time window: September 1, 2026 to October 1, 2026 (UTC)
START_DATE="20260901T000000Z"
END_DATE="20261001T000000Z"

# 1. Discover all personal calendar collection endpoints
CALENDARS=$(curl -s -X PROPFIND \
  -u "${NC_USER}:${NC_PASS}" \
  -H "Depth: 1" \
  -H "Content-Type: application/xml; charset=utf-8" \
  -d '<?xml version="1.0" encoding="utf-8" ?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><d:resourcetype/></d:prop>
</d:propfind>' \
  "https://${NC_HOST}/remote.php/dav/calendars/${NC_USER_URL}/" \
  | grep -oE "<d:href>[^<]+</d:href>" \
  | sed 's#</*d:href>##g' \
  | grep -E "/calendars/(${NC_USER}|${NC_USER_URL})/[^/]+/?$" \
  | grep -vE "/calendars/(${NC_USER}|${NC_USER_URL})/?$")

# 2. Query each calendar and print matching events
for cal_path in $CALENDARS; do
  echo "--- Calendar: ${cal_path} ---"
  curl -s -X REPORT \
    -u "${NC_USER}:${NC_PASS}" \
    -H "Depth: 1" \
    -H "Content-Type: application/xml; charset=utf-8" \
    -d "<?xml version=\"1.0\" encoding=\"utf-8\" ?>
<c:calendar-query xmlns:d=\"DAV:\" xmlns:c=\"urn:ietf:params:xml:ns:caldav\">
  <d:prop>
    <c:calendar-data />
  </d:prop>
  <c:filter>
    <c:comp-filter name=\"VCALENDAR\">
      <c:comp-filter name=\"VEVENT\">
        <c:time-range start=\"${START_DATE}\" end=\"${END_DATE}\"/>
      </c:comp-filter>
    </c:comp-filter>
  </c:filter>
</c:calendar-query>" \
    "https://${NC_HOST}${cal_path}" \
    | grep -E "^(SUMMARY):"
done
