//go:build darwin

package keystore

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"sort"
	"strings"
	"unsafe"
)

func (o *osStore) List(prefix string) ([]string, error) {
	serviceC := C.CString(service)
	defer C.free(unsafe.Pointer(serviceC))

	var attrs []C.SecKeychainAttribute
	attrs = append(attrs, C.SecKeychainAttribute{
		tag:    C.kSecServiceNameAttr,
		length: C.UInt32(len(service)),
		data:   unsafe.Pointer(serviceC),
	})

	searchList := C.SecKeychainAttributeList{
		count: C.UInt32(len(attrs)),
		attr:  &attrs[0],
	}

	var searchRef C.SecKeychainSearchRef
	status := C.SecKeychainSearchCreateFromAttributes(
		nil,
		C.kSecGenericPasswordItemClass,
		&searchList,
		&searchRef,
	)
	if status != C.errSecSuccess {
		return nil, fmt.Errorf("keystore list: SecKeychainSearchCreateFromAttributes: 0x%x", status)
	}
	defer C.CFRelease(C.CFTypeRef(searchRef))

	var keys []string
	for {
		var item C.SecKeychainItemRef
		status = C.SecKeychainSearchCopyNext(searchRef, &item)
		if status == C.errSecItemNotFound {
			break
		}
		if status != C.errSecSuccess {
			continue
		}

		acctTag := C.UInt32(C.kSecAccountItemAttr)
		acctFormat := C.UInt32(0)
		info := C.SecKeychainAttributeInfo{
			count:  1,
			tag:    &acctTag,
			format: &acctFormat,
		}
		var attrList *C.SecKeychainAttributeList
		status = C.SecKeychainItemCopyAttributesAndData(item, nil, &info, nil, &attrList)
		if status == C.errSecSuccess && attrList != nil && attrList.count > 0 {
			attr := attrList.attr[0]
			if attr.length > 0 && attr.data != nil {
				key := C.GoStringN((*C.char)(attr.data), C.int(attr.length))
				if strings.HasPrefix(key, prefix) {
					keys = append(keys, key)
				}
			}
			C.SecKeychainItemFreeAttributesAndData(attrList, nil)
		}
		C.CFRelease(C.CFTypeRef(item))
	}

	sort.Strings(keys)
	return keys, nil
}
