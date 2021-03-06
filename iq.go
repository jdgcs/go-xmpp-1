package xmpp // import "fluux.io/xmpp"

import (
	"encoding/xml"
	"reflect"
	"strconv"

	"fluux.io/xmpp/iot"
)

/*
TODO support ability to put Raw payload inside IQ
*/

// ============================================================================
// XMPP Errors

// Err is an XMPP stanza payload that is used to report error on message,
// presence or iq stanza.
// It is intended to be added in the payload of the erroneous stanza.
type Err struct {
	XMLName xml.Name `xml:"error"`
	Code    int      `xml:"code,attr,omitempty"`
	Type    string   `xml:"type,attr,omitempty"`
	Reason  string
	Text    string `xml:"urn:ietf:params:xml:ns:xmpp-stanzas text,omitempty"`
}

func (*Err) IsIQPayload() {}

// UnmarshalXML implements custom parsing for IQs
func (x *Err) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	x.XMLName = start.Name

	// Extract attributes
	for _, attr := range start.Attr {
		if attr.Name.Local == "type" {
			x.Type = attr.Value
		}
		if attr.Name.Local == "code" {
			if code, err := strconv.Atoi(attr.Value); err == nil {
				x.Code = code
			}
		}
	}

	// Check subelements to extract error text and reason (from local namespace).
	for {
		t, err := d.Token()
		if err != nil {
			return err
		}

		switch tt := t.(type) {

		case xml.StartElement:
			elt := new(Node)

			err = d.DecodeElement(elt, &tt)
			if err != nil {
				return err
			}

			textName := xml.Name{Space: "urn:ietf:params:xml:ns:xmpp-stanzas", Local: "text"}
			if elt.XMLName == textName {
				x.Text = string(elt.Content)
			} else if elt.XMLName.Space == "urn:ietf:params:xml:ns:xmpp-stanzas" {
				x.Reason = elt.XMLName.Local
			}

		case xml.EndElement:
			if tt == start.End() {
				return nil
			}
		}
	}
}

func (x Err) MarshalXML(e *xml.Encoder, start xml.StartElement) (err error) {
	if x.Code == 0 {
		return nil
	}

	// Encode start element and attributes
	start.Name = xml.Name{Local: "error"}

	code := xml.Attr{
		Name:  xml.Name{Local: "code"},
		Value: strconv.Itoa(x.Code),
	}
	start.Attr = append(start.Attr, code)

	if len(x.Type) > 0 {
		typ := xml.Attr{
			Name:  xml.Name{Local: "type"},
			Value: x.Type,
		}
		start.Attr = append(start.Attr, typ)
	}
	err = e.EncodeToken(start)

	// SubTags
	// Reason
	if x.Reason != "" {
		reason := xml.Name{Space: "urn:ietf:params:xml:ns:xmpp-stanzas", Local: x.Reason}
		e.EncodeToken(xml.StartElement{Name: reason})
		e.EncodeToken(xml.EndElement{Name: reason})
	}

	// Text
	if x.Text != "" {
		text := xml.Name{Space: "urn:ietf:params:xml:ns:xmpp-stanzas", Local: "text"}
		e.EncodeToken(xml.StartElement{Name: text})
		e.EncodeToken(xml.CharData(x.Text))
		e.EncodeToken(xml.EndElement{Name: text})
	}

	return e.EncodeToken(xml.EndElement{Name: start.Name})
}

// ============================================================================
// IQ Packet

type IQ struct { // Info/Query
	XMLName xml.Name `xml:"iq"`
	PacketAttrs
	Payload []IQPayload `xml:",omitempty"`
	RawXML  string      `xml:",innerxml"`
	Error   Err         `xml:"error,omitempty"`
}

func NewIQ(iqtype, from, to, id, lang string) IQ {
	return IQ{
		XMLName: xml.Name{Local: "iq"},
		PacketAttrs: PacketAttrs{
			Id:   id,
			From: from,
			To:   to,
			Type: iqtype,
			Lang: lang,
		},
	}
}

func (iq *IQ) AddPayload(payload IQPayload) {
	iq.Payload = append(iq.Payload, payload)
}

func (iq IQ) MakeError(xerror Err) IQ {
	from := iq.From
	to := iq.To

	iq.Type = "error"
	iq.From = to
	iq.To = from
	iq.Error = xerror

	return iq
}

func (IQ) Name() string {
	return "iq"
}

type iqDecoder struct{}

var iq iqDecoder

func (iqDecoder) decode(p *xml.Decoder, se xml.StartElement) (IQ, error) {
	var packet IQ
	err := p.DecodeElement(&packet, &se)
	return packet, err
}

// UnmarshalXML implements custom parsing for IQs
func (iq *IQ) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	iq.XMLName = start.Name

	// Extract IQ attributes
	for _, attr := range start.Attr {
		if attr.Name.Local == "id" {
			iq.Id = attr.Value
		}
		if attr.Name.Local == "type" {
			iq.Type = attr.Value
		}
		if attr.Name.Local == "to" {
			iq.To = attr.Value
		}
		if attr.Name.Local == "from" {
			iq.From = attr.Value
		}
		if attr.Name.Local == "lang" {
			iq.Lang = attr.Value
		}
	}

	// decode inner elements
	level := 0
	for {
		t, err := d.Token()
		if err != nil {
			return err
		}

		switch tt := t.(type) {

		case xml.StartElement:
			level++
			if level <= 1 {
				var elt interface{}
				payloadType := tt.Name.Space + " " + tt.Name.Local
				if payloadType := typeRegistry[payloadType]; payloadType != nil {
					val := reflect.New(payloadType)
					elt = val.Interface()
				} else {
					elt = new(Node)
				}

				if iqPl, ok := elt.(IQPayload); ok {
					err = d.DecodeElement(elt, &tt)
					if err != nil {
						return err
					}
					iq.Payload = append(iq.Payload, iqPl)
				}
			}

		case xml.EndElement:
			level--
			if tt == start.End() {
				return nil
			}
		}
	}
}

// ============================================================================
// Generic IQ Payload

type IQPayload interface {
	IsIQPayload()
}

// Node is a generic structure to represent XML data. It is used to parse
// unreferenced or custom stanza payload.
type Node struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:"-"`
	Content string     `xml:",innerxml"`
	Nodes   []Node     `xml:",any"`
}

// Attr represents generic XML attributes, as used on the generic XML Node
// representation.
type Attr struct {
	K string
	V string
}

// UnmarshalXML is a custom unmarshal function used by xml.Unmarshal to
// transform generic XML content into hierarchical Node structure.
func (n *Node) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	// Assign	"n.Attrs = start.Attr", without repeating xmlns in attributes:
	for _, attr := range start.Attr {
		// Do not repeat xmlns, it is already in XMLName
		if attr.Name.Local != "xmlns" {
			n.Attrs = append(n.Attrs, attr)
		}
	}
	type node Node
	return d.DecodeElement((*node)(n), &start)
}

// MarshalXML is a custom XML serializer used by xml.Marshal to serialize a
// Node structure to XML.
func (n Node) MarshalXML(e *xml.Encoder, start xml.StartElement) (err error) {
	start.Attr = n.Attrs
	start.Name = n.XMLName

	err = e.EncodeToken(start)
	e.EncodeElement(n.Nodes, xml.StartElement{Name: n.XMLName})
	return e.EncodeToken(xml.EndElement{Name: start.Name})
}

func (*Node) IsIQPayload() {}

// ============================================================================
// Disco

const (
	NSDiscoInfo  = "http://jabber.org/protocol/disco#info"
	NSDiscoItems = "http://jabber.org/protocol/disco#items"
)

type DiscoInfo struct {
	XMLName  xml.Name  `xml:"http://jabber.org/protocol/disco#info query"`
	Node     string    `xml:"node,attr,omitempty"`
	Identity Identity  `xml:"identity"`
	Features []Feature `xml:"feature"`
}

func (*DiscoInfo) IsIQPayload() {}

type Identity struct {
	XMLName  xml.Name `xml:"identity,omitempty"`
	Name     string   `xml:"name,attr,omitempty"`
	Category string   `xml:"category,attr,omitempty"`
	Type     string   `xml:"type,attr,omitempty"`
}

type Feature struct {
	XMLName xml.Name `xml:"feature"`
	Var     string   `xml:"var,attr"`
}

// ============================================================================

type DiscoItems struct {
	XMLName xml.Name    `xml:"http://jabber.org/protocol/disco#items query"`
	Node    string      `xml:"node,attr,omitempty"`
	Items   []DiscoItem `xml:"item"`
}

func (*DiscoItems) IsIQPayload() {}

type DiscoItem struct {
	XMLName xml.Name `xml:"item"`
	Name    string   `xml:"name,attr,omitempty"`
	JID     string   `xml:"jid,attr,omitempty"`
	Node    string   `xml:"node,attr,omitempty"`
}

// ============================================================================

var typeRegistry = make(map[string]reflect.Type)

func init() {
	typeRegistry["http://jabber.org/protocol/disco#info query"] = reflect.TypeOf(DiscoInfo{})
	typeRegistry["http://jabber.org/protocol/disco#items query"] = reflect.TypeOf(DiscoItems{})
	typeRegistry["urn:ietf:params:xml:ns:xmpp-bind bind"] = reflect.TypeOf(BindBind{})
	typeRegistry["urn:xmpp:iot:control set"] = reflect.TypeOf(iot.ControlSet{})
}
