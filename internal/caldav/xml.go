package caldav

import "encoding/xml"

// Namespaces used by CalDAV servers.
const (
	nsDAV      = "DAV:"
	nsCalDAV   = "urn:ietf:params:xml:ns:caldav"
	nsCalServ  = "http://calendarserver.org/ns/"
	nsAppleICS = "http://apple.com/ns/ical/"
)

type multistatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

type davResponse struct {
	Href      string     `xml:"DAV: href"`
	Propstats []propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Status string  `xml:"DAV: status"`
	Prop   davProp `xml:"DAV: prop"`
}

type davProp struct {
	ETag         string       `xml:"DAV: getetag"`
	DisplayName  string       `xml:"DAV: displayname"`
	CTag         string       `xml:"http://calendarserver.org/ns/ getctag"`
	Color        string       `xml:"http://apple.com/ns/ical/ calendar-color"`
	CalendarData string       `xml:"urn:ietf:params:xml:ns:caldav calendar-data"`
	ResourceType resourceType `xml:"DAV: resourcetype"`

	CurrentUserPrincipal *hrefProp `xml:"DAV: current-user-principal"`
	CalendarHomeSet      *hrefProp `xml:"urn:ietf:params:xml:ns:caldav calendar-home-set"`
}

// Discovery response shapes. current-user-principal and calendar-home-set both
// wrap a single href.
type hrefProp struct {
	Href string `xml:"DAV: href"`
}

type resourceType struct {
	Calendar   *struct{} `xml:"urn:ietf:params:xml:ns:caldav calendar"`
	Collection *struct{} `xml:"DAV: collection"`
}

// Request bodies. These are written as literal XML rather than marshalled:
// the shapes are fixed, and the literal form is far easier to compare against
// a server's actual wire traffic when debugging.
const propfindCalendarProps = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:cs="http://calendarserver.org/ns/" xmlns:ic="http://apple.com/ns/ical/">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
    <cs:getctag/>
    <ic:calendar-color/>
  </d:prop>
</d:propfind>`

const propfindObjectETags = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:getetag/>
    <d:resourcetype/>
  </d:prop>
</d:propfind>`

const propfindCurrentUserPrincipal = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop><d:current-user-principal/></d:prop>
</d:propfind>`

const propfindCalendarHomeSet = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav">
  <d:prop><c:calendar-home-set/></d:prop>
</d:propfind>`
