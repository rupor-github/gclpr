#!/bin/bash -e

# Standard preambule
plain() {
    local mesg=$1; shift
    printf "    ${mesg}\n" "$@" >&2
}

print_warning() {
    local mesg=$1; shift
    printf "${YELLOW}=> WARNING: ${mesg}${ALL_OFF}\n" "$@" >&2
}

print_msg1() {
    local mesg=$1; shift
    printf "${GREEN}==> ${mesg}${ALL_OFF}\n" "$@" >&2
}

print_msg2() {
    local mesg=$1; shift
    printf "${BLUE}  -> ${mesg}${ALL_OFF}\n" "$@" >&2
}

print_error() {
    local mesg=$1; shift
    printf "${RED}==> ERROR: ${mesg}${ALL_OFF}\n" "$@" >&2
}

ALL_OFF='[00m'
BLUE='[38;5;04m'
GREEN='[38;5;02m'
RED='[38;5;01m'
YELLOW='[38;5;03m'

readonly ALL_OFF BOLD BLUE GREEN RED YELLOW
ARCH_INSTALLS="${ARCH_INSTALLS:-win32 win64 darwin linux_i386 linux_amd64}"

if not command -v cmake >/dev/null 2>&1; then
    print_error "No cmake found - please, install"
    exit 1
fi

if [ "$1" = "" ]; then
    mk=make
else
    mk=ninja
fi
if not command -v ${mk} >/dev/null 2>&1; then
    print_error "No ${mk} found - please, install"
    exit 1
fi

for _arch in ${ARCH_INSTALLS}; do

    _dist=bin_${_arch}

    echo
    echo
    print_msg1 "Building ${_arch} release"

    [ -d ${_dist} ] && rm -rf ${_dist}

    [ -d build_${_arch} ] && rm -rf build_${_arch}
    mkdir -p build_${_arch}

    (
        cd  build_${_arch}

        if [[ ${_arch} == linux* ]]; then
            MSYSTEM_NAME=${_arch} cmake -DCMAKE_BUILD_TYPE=Release ..
        else
            cmake -DCMAKE_BUILD_TYPE=Release -DCMAKE_TOOLCHAIN_FILE=cmake/${_arch}.toolchain ..
        fi
        ${mk} install
    )
    (
        [ -f gclpr_${_arch}.zip ] && rm gclpr_${_arch}.zip
        [ -f gclpr_${_arch}.zip.minisig ] && rm gclpr_${_arch}.zip.minisig
        cd ${_dist}
        zip -9 ../gclpr_${_arch}.zip *
        cd ..
        echo ${BUILD_PSWD} | minisign -S -s ~/.minisign/build.key -c "gclpr for ${_arch} release signature" -m gclpr_${_arch}.zip
    )
done

exit 0

