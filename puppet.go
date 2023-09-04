// mautrix-gmessages - A Matrix-Google Messages puppeting bridge.
// Copyright (C) 2023 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/bridge"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-gmessages/database"
	"go.mau.fi/mautrix-gmessages/libgm/gmproto"
)

var userIDRegex *regexp.Regexp

func (br *GMBridge) ParsePuppetMXID(mxid id.UserID) (key database.Key, ok bool) {
	if userIDRegex == nil {
		userIDRegex = br.Config.MakeUserIDRegex(`([0-9]+)\.([0-9]+)`)
	}
	match := userIDRegex.FindStringSubmatch(string(mxid))
	if len(match) == 3 {
		var err error
		key.Receiver, err = strconv.Atoi(match[1])
		ok = err == nil
		key.ID = match[2]
	}
	return
}

func (br *GMBridge) GetPuppetByMXID(mxid id.UserID) *Puppet {
	key, ok := br.ParsePuppetMXID(mxid)
	if !ok {
		return nil
	}

	return br.GetPuppetByKey(key, "")
}

func (br *GMBridge) DeleteAllPuppetsForUser(userID int) {
	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()
	err := br.DB.Puppet.DeleteAllForUser(context.Background(), userID)
	if err != nil {
		br.ZLog.Err(err).Msg("Failed to delete all ghosts for user from database")
	}
	for key, puppet := range br.puppetsByKey {
		if puppet.Receiver == userID {
			delete(br.puppetsByKey, key)
		}
	}
}

func (br *GMBridge) GetPuppetByKey(key database.Key, phone string) *Puppet {
	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()
	puppet, ok := br.puppetsByKey[key]
	if !ok {
		dbPuppet, err := br.DB.Puppet.Get(context.TODO(), key)
		if err != nil {
			br.ZLog.Err(err).Object("puppet_key", key).Msg("Failed to get puppet from database")
			return nil
		}
		if dbPuppet == nil {
			if phone == "" {
				return nil
			}
			dbPuppet = br.DB.Puppet.New()
			dbPuppet.Key = key
			dbPuppet.Phone = phone
			err = dbPuppet.Insert(context.TODO())
			if err != nil {
				br.ZLog.Err(err).Object("puppet_key", key).Msg("Failed to insert puppet into database")
				return nil
			}
		}
		puppet = br.NewPuppet(dbPuppet)
		br.puppetsByKey[puppet.Key] = puppet
	}
	return puppet
}

func (br *GMBridge) IsGhost(id id.UserID) bool {
	_, ok := br.ParsePuppetMXID(id)
	return ok
}

func (br *GMBridge) GetIGhost(id id.UserID) bridge.Ghost {
	p := br.GetPuppetByMXID(id)
	if p == nil {
		return nil
	}
	return p
}

func (puppet *Puppet) GetMXID() id.UserID {
	return puppet.MXID
}

func (br *GMBridge) GetAllPuppets() []*Puppet {
	return br.loadManyPuppets(br.DB.Puppet.GetAll)
}

func (br *GMBridge) loadManyPuppets(query func(ctx context.Context) ([]*database.Puppet, error)) []*Puppet {
	br.puppetsLock.Lock()
	defer br.puppetsLock.Unlock()
	dbPuppets, err := query(context.TODO())
	if err != nil {
		br.ZLog.Err(err).Msg("Failed to load all puppets from database")
		return []*Puppet{}
	}
	output := make([]*Puppet, len(dbPuppets))
	for index, dbPuppet := range dbPuppets {
		if dbPuppet == nil {
			continue
		}
		puppet, ok := br.puppetsByKey[dbPuppet.Key]
		if !ok {
			puppet = br.NewPuppet(dbPuppet)
			br.puppetsByKey[puppet.Key] = puppet
		}
		output[index] = puppet
	}
	return output
}

func (br *GMBridge) FormatPuppetMXID(key database.Key) id.UserID {
	return id.NewUserID(
		br.Config.Bridge.FormatUsername(key.String()),
		br.Config.Homeserver.Domain)
}

func (br *GMBridge) NewPuppet(dbPuppet *database.Puppet) *Puppet {
	return &Puppet{
		Puppet: dbPuppet,
		bridge: br,
		log: br.ZLog.With().
			Str("phone", dbPuppet.Phone).
			Str("puppet_id", dbPuppet.ID).
			Int("puppet_receiver", dbPuppet.Receiver).
			Logger(),
		MXID: br.FormatPuppetMXID(dbPuppet.Key),
	}
}

type Puppet struct {
	*database.Puppet
	bridge *GMBridge
	log    zerolog.Logger
	MXID   id.UserID
}

var _ bridge.GhostWithProfile = (*Puppet)(nil)

func (puppet *Puppet) GetDisplayname() string {
	return puppet.Name
}

func (puppet *Puppet) GetAvatarURL() id.ContentURI {
	return puppet.AvatarMXC
}

func (puppet *Puppet) SwitchCustomMXID(_ string, _ id.UserID) error {
	return fmt.Errorf("puppets don't support custom MXIDs here")
}

func (puppet *Puppet) ClearCustomMXID() {}

func (puppet *Puppet) IntentFor(_ *Portal) *appservice.IntentAPI {
	return puppet.DefaultIntent()
}

func (puppet *Puppet) CustomIntent() *appservice.IntentAPI {
	return nil
}

func (puppet *Puppet) DefaultIntent() *appservice.IntentAPI {
	return puppet.bridge.AS.Intent(puppet.MXID)
}

const MinAvatarUpdateInterval = 24 * time.Hour

func (puppet *Puppet) UpdateAvatar(source *User) bool {
	if (puppet.AvatarSet && time.Since(puppet.AvatarUpdateTS) < MinAvatarUpdateInterval) || puppet.ContactID == "" {
		return false
	}
	resp, err := source.Client.GetParticipantThumbnail(puppet.ID)
	if err != nil {
		puppet.log.Err(err).Msg("Failed to get avatar thumbnail")
		return false
	}
	puppet.AvatarUpdateTS = time.Now()
	if len(resp.Thumbnail) == 0 {
		if puppet.AvatarHash == [32]byte{} {
			return true
		}
		puppet.AvatarHash = [32]byte{}
		puppet.AvatarMXC = id.ContentURI{}
		puppet.AvatarSet = false
	} else {
		thumbData := resp.Thumbnail[0].GetData()
		hash := sha256.Sum256(thumbData.GetImageBuffer())
		if hash == puppet.AvatarHash {
			return true
		}
		puppet.AvatarHash = hash
		puppet.AvatarSet = false
		avatarBytes := thumbData.GetImageBuffer()
		uploadResp, err := puppet.DefaultIntent().UploadMedia(mautrix.ReqUploadMedia{
			ContentBytes: avatarBytes,
			ContentType:  http.DetectContentType(avatarBytes),
		})
		if err != nil {
			puppet.log.Err(err).Msg("Failed to upload avatar")
			return true
		}
		puppet.AvatarMXC = uploadResp.ContentURI
	}
	err = puppet.DefaultIntent().SetAvatarURL(puppet.AvatarMXC)
	if err != nil {
		puppet.log.Err(err).Msg("Failed to set avatar")
	} else {
		puppet.AvatarSet = true
	}
	go puppet.updatePortalAvatar()
	return true
}

func (puppet *Puppet) UpdateName(formattedPhone, fullName, firstName string) bool {
	newName := puppet.bridge.Config.Bridge.FormatDisplayname(formattedPhone, fullName, firstName)
	if puppet.Name != newName || !puppet.NameSet {
		oldName := puppet.Name
		puppet.Name = newName
		puppet.NameSet = false
		err := puppet.DefaultIntent().SetDisplayName(newName)
		if err == nil {
			puppet.log.Debug().Str("old_name", oldName).Str("new_name", newName).Msg("Updated displayname")
			puppet.NameSet = true
			go puppet.updatePortalName()
		} else {
			puppet.log.Warn().Err(err).Msg("Failed to set displayname")
		}
		return true
	}
	return false
}

func (puppet *Puppet) UpdateContactInfo() bool {
	if !puppet.bridge.SpecVersions.Supports(mautrix.BeeperFeatureArbitraryProfileMeta) {
		return false
	}

	if puppet.ContactInfoSet {
		return false
	}

	contactInfo := map[string]any{
		"com.beeper.bridge.identifiers": []string{
			fmt.Sprintf("tel:%s", puppet.Phone),
			fmt.Sprintf("gmsg-contact:%s", puppet.ContactID),
		},
		"com.beeper.bridge.remote_id": puppet.Key.String(),
		"com.beeper.bridge.service":   "gmessages",
		"com.beeper.bridge.network":   "gmessages",
	}
	err := puppet.DefaultIntent().BeeperUpdateProfile(contactInfo)
	if err != nil {
		puppet.log.Warn().Err(err).Msg("Failed to store custom contact info in profile")
		return false
	} else {
		puppet.ContactInfoSet = true
		return true
	}
}

func (puppet *Puppet) updatePortalAvatar() {
	// TODO implement
}

func (puppet *Puppet) updatePortalName() {
	// TODO implement
}

func (puppet *Puppet) Sync(source *User, contact *gmproto.Participant) {
	err := puppet.DefaultIntent().EnsureRegistered()
	if err != nil {
		puppet.log.Err(err).Msg("Failed to ensure registered")
	}

	update := false
	if contact.ID.Number != "" && puppet.Phone != contact.ID.Number {
		puppet.Phone = contact.ID.Number
		update = true
	}
	if contact.ContactID != puppet.ContactID {
		puppet.ContactID = contact.ContactID
		update = true
	}
	update = puppet.UpdateName(contact.GetFormattedNumber(), contact.GetFullName(), contact.GetFirstName()) || update
	update = puppet.UpdateAvatar(source) || update
	update = puppet.UpdateContactInfo() || update
	if update {
		err = puppet.Update(context.TODO())
		if err != nil {
			puppet.log.Err(err).Msg("Failed to save puppet to database after sync")
		}
	}
}
