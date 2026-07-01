//go:build darwin

package keystore

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Security
#include <CoreFoundation/CoreFoundation.h>
#include <Security/Security.h>
#include <stdlib.h>

static CFArrayRef copy_all_matching(const char *service) {
    CFStringRef svc = CFStringCreateWithCString(
        kCFAllocatorDefault, service, kCFStringEncodingUTF8);
    if (!svc) return NULL;

    const void *keys[] = {
        kSecClass, kSecAttrService, kSecMatchLimit, kSecReturnAttributes,
    };
    const void *vals[] = {
        kSecClassGenericPassword, svc, kSecMatchLimitAll, kCFBooleanTrue,
    };

    CFDictionaryRef query = CFDictionaryCreate(
        kCFAllocatorDefault, keys, vals, 4,
        &kCFTypeDictionaryKeyCallBacks,
        &kCFTypeDictionaryValueCallBacks);
    CFRelease(svc);
    if (!query) return NULL;

    CFTypeRef result = NULL;
    OSStatus status = SecItemCopyMatching(query, &result);
    CFRelease(query);
    if (status == errSecItemNotFound) return NULL;
    if (status != errSecSuccess) return NULL;
    return (CFArrayRef)result;
}

static long cfarray_count(CFArrayRef arr) {
    return (long)CFArrayGetCount(arr);
}

static CFDictionaryRef cfarray_get_dict(CFArrayRef arr, long idx) {
    return (CFDictionaryRef)CFArrayGetValueAtIndex(arr, idx);
}

static char *copy_account_from_dict(CFDictionaryRef dict) {
    CFStringRef acct = CFDictionaryGetValue(dict, kSecAttrAccount);
    if (!acct) return NULL;
    CFIndex len = CFStringGetLength(acct);
    CFIndex maxSize = CFStringGetMaximumSizeForEncoding(len, kCFStringEncodingUTF8) + 1;
    char *buf = (char *)malloc((size_t)maxSize);
    if (!buf) return NULL;
    if (!CFStringGetCString(acct, buf, maxSize, kCFStringEncodingUTF8)) {
        free(buf);
        return NULL;
    }
    return buf;
}
*/
import "C"
import (
	"sort"
	"strings"
	"unsafe"
)

func (o *osStore) List(prefix string) ([]string, error) {
	serviceC := C.CString(service)
	defer C.free(unsafe.Pointer(serviceC))

	items := C.copy_all_matching(serviceC)
	if items == nil {
		return nil, nil
	}
	defer C.CFRelease(C.CFTypeRef(items))

	count := int(C.cfarray_count(items))
	keys := make([]string, 0, count)
	for i := 0; i < count; i++ {
		dict := C.cfarray_get_dict(items, C.long(i))
		acct := C.copy_account_from_dict(dict)
		if acct == nil {
			continue
		}
		key := C.GoString(acct)
		C.free(unsafe.Pointer(acct))
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}

	if len(keys) == 0 {
		return nil, nil
	}
	sort.Strings(keys)
	return keys, nil
}
