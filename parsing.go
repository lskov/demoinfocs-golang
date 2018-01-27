package demoinfocs

import (
	"errors"
	"fmt"
	"os"

	common "github.com/markus-wa/demoinfocs-golang/common"
	events "github.com/markus-wa/demoinfocs-golang/events"
)

const maxOsPath = 260

const (
	playerWeaponPrefix    = "m_hMyWeapons."
	playerWeaponPrePrefix = "bcc_nonlocaldata."
)

// ParseHeader attempts to parse the header of the demo.
// Returns error if the filestamp (first 8 bytes) doesn't match HL2DEMO.
func (p *Parser) ParseHeader() error {
	var h common.DemoHeader
	h.Filestamp = p.bitReader.ReadCString(8)
	h.Protocol = p.bitReader.ReadSignedInt(32)
	h.NetworkProtocol = p.bitReader.ReadSignedInt(32)
	h.ServerName = p.bitReader.ReadCString(maxOsPath)
	h.ClientName = p.bitReader.ReadCString(maxOsPath)
	h.MapName = p.bitReader.ReadCString(maxOsPath)
	h.GameDirectory = p.bitReader.ReadCString(maxOsPath)
	h.PlaybackTime = p.bitReader.ReadFloat()
	h.PlaybackTicks = p.bitReader.ReadSignedInt(32)
	h.PlaybackFrames = p.bitReader.ReadSignedInt(32)
	h.SignonLength = p.bitReader.ReadSignedInt(32)

	if h.Filestamp != "HL2DEMO" {
		return errors.New("Invalid File-Type; expecting HL2DEMO in the first 8 bytes")
	}

	p.header = &h
	p.eventDispatcher.Dispatch(events.HeaderParsedEvent{Header: h})
	return nil
}

// ParseToEnd attempts to parse the demo until the end.
// Aborts and returns an error if Cancel() is called before the end.
// May panic if the demo is corrupt in some way.
func (p *Parser) ParseToEnd() error {
	for {
		select {
		case <-p.cancelChan:
			return errors.New("Parsing was cancelled before it finished")

		default:
			if !p.ParseNextFrame() {
				return nil
			}
		}
	}
}

// Cancel aborts ParseToEnd() on the upcoming tick.
func (p *Parser) Cancel() {
	p.cancelChan <- struct{}{}
}

// ParseNextFrame attempts to parse the next frame / demo-tick (not ingame tick).
// Returns true unless the demo command 'stop' was encountered.
// Panics if header hasn't been parsed yet - see Parser.ParseHeader().
func (p *Parser) ParseNextFrame() bool {
	if p.header == nil {
		panic("Tried to parse tick before parsing header")
	}
	b := p.parseFrame()

	for k, rp := range p.rawPlayers {
		if rp == nil {
			continue
		}

		if pl := p.players[k]; pl != nil {
			newPlayer := false
			if p.connectedPlayers[rp.UserID] == nil {
				p.connectedPlayers[rp.UserID] = pl
				newPlayer = true
			}

			pl.Name = rp.Name
			pl.SteamID = rp.XUID
			pl.IsBot = rp.IsFakePlayer
			pl.AdditionalPlayerInformation = &p.additionalPlayerInfo[pl.EntityID]

			if pl.IsAlive() {
				pl.LastAlivePosition = pl.Position
			}

			if newPlayer && pl.SteamID != 0 {
				p.eventDispatcher.Dispatch(events.PlayerBindEvent{Player: pl})
			}
		}
	}

	p.eventDispatcher.Dispatch(events.TickDoneEvent{})

	if !b {
		close(p.msgQueue)
	}

	return b
}

func (p *Parser) parseFrame() bool {
	cmd := demoCommand(p.bitReader.ReadSingleByte())

	// Ingame tick number
	p.ingameTick = p.bitReader.ReadSignedInt(32)
	// Skip 'player slot'
	p.bitReader.ReadSingleByte()

	p.currentFrame++

	switch cmd {
	case dcSynctick:
		// Ignore

	case dcStop:
		return false

	case dcConsoleCommand:
		// Skip
		p.bitReader.BeginChunk(p.bitReader.ReadSignedInt(32) << 3)
		p.bitReader.EndChunk()

	case dcDataTables:
		p.bitReader.BeginChunk(p.bitReader.ReadSignedInt(32) << 3)
		p.stParser.ParsePacket(p.bitReader)
		p.bitReader.EndChunk()

		p.mapEquipment()
		p.bindEntities()

	case dcStringTables:
		p.parseStringTables()

	case dcUserCommand:
		// Skip
		p.bitReader.ReadInt(32)
		p.bitReader.BeginChunk(p.bitReader.ReadSignedInt(32) << 3)
		p.bitReader.EndChunk()

	case dcSignon:
		fallthrough
	case dcPacket:
		p.parsePacket()

	case dcCustomData:
		fmt.Fprintf(os.Stderr, "WARNING: Found CustomData but not handled\n")

	default:
		panic(fmt.Sprintf("Canny handle it anymoe (command %v unknown)", cmd))
	}
	return true
}
