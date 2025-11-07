// pkg/plciface/plcwriter.go
package plc_interface

import "context"

type PLCWriter interface {
    WritePLC(ctx context.Context, deviceStr string, value any) error
}
