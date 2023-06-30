package textgapi

import (
	"go.mau.fi/mautrix-gmessages/libgm/binary"
	"go.mau.fi/mautrix-gmessages/libgm/events"
)

func (c *Client) handleUserAlertEvent(response *Response, evtData *binary.Event_UserAlertEvent) {
	switch evtData.UserAlertEvent.AlertType {
	case 2:
		browserActive := events.NewBrowserActive(response.Data.RequestId)
		c.triggerEvent(browserActive)
		return
	case 5, 6:
		batteryEvt := events.NewBattery()
		c.triggerEvent(batteryEvt)
		return
	case 3, 4:
		dataConnectionEvt := events.NewDataConnection()
		c.triggerEvent(dataConnectionEvt)
		return
	}
}
