package raw

/*
#cgo CFLAGS: -I${SRCDIR}/../../../core
#include "moonshine-c-api.h"
*/
import "C"

const HeaderVersion = int32(C.MOONSHINE_HEADER_VERSION)
