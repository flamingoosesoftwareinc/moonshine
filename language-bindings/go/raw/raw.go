package raw

/*
#cgo CFLAGS: -I${SRCDIR}/../../../core
#include "moonshine-c-api.h"
*/
import "C"

const MoonshineHeaderVersion = int32(C.MOONSHINE_HEADER_VERSION)
