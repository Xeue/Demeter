package scan

import (
	"strconv"
	"strings"

	"github.com/Xeue/Demeter/internal/expr"
	"github.com/Xeue/Demeter/internal/model"
)

// computeGroupCommands ports main.ts:812-842. It expands a frame's assigned
// group into per-command desired values, substituting the FRAME/SLOT/CARD/SPIGOT
// keywords and evaluating expressions. A missing group yields no commands (the
// nil-map-panic guard the legacy code lacked).
func computeGroupCommands(groupName, frameNumber, slotNum, frameIP string, groups model.Groups, events Events) map[string]model.FrameGroup {
	out := map[string]model.FrameGroup{}
	g := groups[groupName]
	if g == nil {
		return out
	}
	slotNumber := mustAtoi(slotNum)
	for commandID, command := range g.Commands {
		if !command.Enabled {
			continue
		}
		value := command.Value.String()
		value = strings.ReplaceAll(value, "FRAME", frameNumber)
		value = strings.ReplaceAll(value, "SLOT", slotNum)
		value = strings.ReplaceAll(value, "CARD", strconv.Itoa(slotNumber/2))

		if command.Type == "card" {
			cmd, err := parseCommand(value, command.DataType, command.Take)
			if err != nil {
				events.FrameError(frameIP, err.Error())
				continue
			}
			out[commandID] = cmd
			continue
		}
		// spigot: expand across 16 spigots with increment offsets
		inc := int(command.Increment)
		base := mustAtoi(commandID)
		for spigot := 0; spigot < 16; spigot++ {
			take := model.Num(int(command.Take) + inc*spigot)
			v := strings.ReplaceAll(value, "SPIGOT", strconv.Itoa(spigot+1))
			cmd, err := parseCommand(v, command.DataType, take)
			if err != nil {
				events.FrameError(frameIP, err.Error())
				continue
			}
			out[strconv.Itoa(base+inc*spigot)] = cmd
		}
	}
	return out
}

// parseCommand ports main.ts:844-867. smartip evaluates each octet (strict);
// everything else evaluates the whole value, falling back to the literal string.
func parseCommand(value, dataType string, take model.Num) (model.FrameGroup, error) {
	if dataType == "smartip" {
		ip, err := expr.EvalSmartIP(value)
		if err != nil {
			return model.FrameGroup{}, err
		}
		return model.FrameGroup{Value: model.StrVal(ip), Type: dataType, Enabled: true, Take: take}, nil
	}
	return model.FrameGroup{Value: expr.EvalValue(value), Type: dataType, Enabled: true, Take: take}, nil
}
