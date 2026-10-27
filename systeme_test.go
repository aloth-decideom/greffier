package main

import "testing"

func TestDescriptionSource(t *testing.T) {
	liste := `Source #3538
	State: RUNNING
	Name: alsa_output.usb-Jabra.analog-stereo.monitor
	Description: Monitor of Jabra Stéréo analogique
	Driver: PipeWire

Source #3539
	Name: alsa_input.usb-Jabra.mono-fallback
	Description: Jabra Mono

Source #4098
	Name: alsa_output.pci.Speaker__sink.monitor
	Description: Monitor of Meteor Lake-P HD Audio Controller Speaker
`
	cas := map[string]string{
		"alsa_output.usb-Jabra.analog-stereo.monitor": "Monitor of Jabra Stéréo analogique",
		"alsa_output.pci.Speaker__sink.monitor":       "Monitor of Meteor Lake-P HD Audio Controller Speaker",
		"inconnue.monitor":                            "",
	}
	for nom, attendu := range cas {
		if got := descriptionSource(liste, nom); got != attendu {
			t.Errorf("descriptionSource(%q) = %q, attendu %q", nom, got, attendu)
		}
	}
}
