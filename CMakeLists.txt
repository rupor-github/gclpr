cmake_minimum_required(VERSION 3.5)

project(gclpr NONE)

message(STATUS "Running cmake version ${CMAKE_VERSION}")
if (NOT CMAKE_BUILD_TYPE)
    set(CMAKE_BUILD_TYPE "Release" CACHE STRING "default to release build type" FORCE)
endif()
set_property(CACHE CMAKE_BUILD_TYPE PROPERTY STRINGS "Debug" "Release")
message(STATUS "Build type: ${CMAKE_BUILD_TYPE}")
message(STATUS "Generator: ${CMAKE_GENERATOR}")

set(CMAKE_MODULE_PATH ${CMAKE_MODULE_PATH} ${CMAKE_SOURCE_DIR}/cmake)

# Release installation directory
if(CMAKE_CROSSCOMPILING)
    if(MSYSTEM_NAME STREQUAL "win32")
        set(CROSS_GOOS windows)
        set(CROSS_GOARCH 386)
        message(STATUS "Targeting 32 bits Windows executable")
    elseif(MSYSTEM_NAME STREQUAL "win64")
        set(CROSS_GOOS windows)
        set(CROSS_GOARCH amd64)
        message(STATUS "Targeting 64 bits Windows executable")
    elseif(MSYSTEM_NAME STREQUAL "darwin_amd64")
        set(CROSS_GOOS darwin)
        set(CROSS_GOARCH amd64)
        message(STATUS "Targeting 64 bits Darwin AMD executable")
    elseif(MSYSTEM_NAME STREQUAL "darwin_arm64")
        set(CROSS_GOOS darwin)
        set(CROSS_GOARCH arm64)
        message(STATUS "Targeting 64 bits Darwin ARM executable")
    else()
        message(FATAL_ERROR "Currently unsuppored environment \"${MSYSTEM_NAME}\". CMake willl exit now.")
    endif()
else()
    if (NOT "$ENV{MSYSTEM_NAME}" STREQUAL "")
        set(MSYSTEM_NAME "$ENV{MSYSTEM_NAME}" CACHE INTERNAL "Copied from environment")
    else()
        set(MSYSTEM_NAME "linux_amd64" CACHE INTERNAL "Host environment")
    endif()
    if("$ENV{MSYSTEM_NAME}" STREQUAL "linux_i386")
        set(CROSS_GOOS linux)
        set(CROSS_GOARCH 386)
        message(STATUS "Targeting 32 bits Linux executable")
    else()
        # host platform is always linux amd64
        message(STATUS "Targeting 64 bits Linux executable")
    endif()
endif()

set(GO_MIN_REQURED_VERSION 1.19)
find_package(Go ${GO_MIN_REQURED_VERSION} REQUIRED)
find_package(Git REQUIRED)

# Make sure we setup go properly
if (DEFINED ENV{GOPATH})
    set(GO_PATH "$ENV{GOPATH}")
else()
    set(GO_PATH "/tmp/gopkg_${CMAKE_PROJECT_NAME}")
    execute_process(COMMAND ${CMAKE_COMMAND} -E make_directory ${GO_PATH})
endif()
message(STATUS "Using go modules with GOPATH=${GO_PATH}")
if(GO_PATH MATCHES "^${PROJECT_SOURCE_DIR}.*")
    message(FATAL_ERROR "We are working in \"go modules outside of GOPATH mode\" - GOPATH should be outside of project source directory!")
endif()

########################################################################################################
# Setting up versioning
########################################################################################################

# Project version number
set(PRJ_VERSION_Major "1")
set(PRJ_VERSION_Minor "3")
set(PRJ_VERSION_Patch "2")

if (EXISTS "${PROJECT_SOURCE_DIR}/.git" AND IS_DIRECTORY "${PROJECT_SOURCE_DIR}/.git")
    execute_process(COMMAND ${CMAKE_SOURCE_DIR}/cmake/githash.sh ${GIT_EXECUTABLE}
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        RESULT_VARIABLE res
        OUTPUT_VARIABLE GIT_HASH
        ERROR_QUIET
        OUTPUT_STRIP_TRAILING_WHITESPACE)
    # This will cause reconfiguration on every commit
    set_property(DIRECTORY APPEND PROPERTY CMAKE_CONFIGURE_DEPENDS "${PROJECT_SOURCE_DIR}/.git/index")
else()
    set(GIT_HASH "no-git")
endif()
configure_file("${PROJECT_SOURCE_DIR}/cmake/version.go.in" "${PROJECT_SOURCE_DIR}/misc/version.go")
if(CROSS_GOOS STREQUAL "windows")
    if(NOT EXISTS "${PROJECT_SOURCE_DIR}/gclpr.json")
        configure_file("${PROJECT_SOURCE_DIR}/cmake/gclpr.json.in" "${PROJECT_SOURCE_DIR}/gclpr.json")
    endif()
    configure_file("${PROJECT_SOURCE_DIR}/cmake/gui.rc.in" "${PROJECT_SOURCE_DIR}/cmd/gui/resources.rc")
    configure_file("${PROJECT_SOURCE_DIR}/cmake/gui.xml.in" "${PROJECT_SOURCE_DIR}/cmd/gui/manifest.xml")
    # for user convinience
    execute_process(COMMAND ${CMAKE_COMMAND} -E copy ${CMAKE_CURRENT_SOURCE_DIR}/../../jstarks/npiperelay/npiperelay.exe ${PROJECT_BINARY_DIR})
endif()

# distribute history
execute_process(COMMAND ${CMAKE_COMMAND} -E env ${GIT_EXECUTABLE} changelog --all --tag "v${PRJ_VERSION_Major}.${PRJ_VERSION_Minor}.${PRJ_VERSION_Patch}" --stdout OUTPUT_FILE ${PROJECT_BINARY_DIR}/changelog.txt ERROR_QUIET)

message(STATUS "Building version \"${PRJ_VERSION_Major}.${PRJ_VERSION_Minor}.${PRJ_VERSION_Patch}\" git \"${GIT_HASH}\"")

if(CMAKE_BUILD_TYPE STREQUAL "Debug")
    # Create local git hooks
    if(EXISTS "${PROJECT_SOURCE_DIR}/.git" AND IS_DIRECTORY "${PROJECT_SOURCE_DIR}/.git")
        execute_process(COMMAND ${CMAKE_COMMAND} -E copy ${PROJECT_SOURCE_DIR}/cmake/pre-commit ${PROJECT_SOURCE_DIR}/.git/hooks/. WORKING_DIRECTORY ${PROJECT_SOURCE_DIR})
        configure_file("${PROJECT_SOURCE_DIR}/cmake/pre-push" "${PROJECT_SOURCE_DIR}/.git/hooks/pre-push" @ONLY)
    endif()
endif()

########################################################################################################
# Some contexts dependent settings
########################################################################################################

list(APPEND GO_ENV
    GOPATH=${GO_PATH}
    GOOS=${CROSS_GOOS}
    GOARCH=${CROSS_GOARCH}
    CGO_ENABLED=0
    )

if(CMAKE_BUILD_TYPE STREQUAL "Debug")
    set(TEST_RUN_ARGS "-v")
    set(GO_MOD_ARGS "-mod=mod")
else()
    set(GO_MOD_ARGS "-mod=vendor")
endif()

########################################################################################################
# main target - always out of date, "go build" will figure it out
########################################################################################################

list(APPEND REL_DEPS ${PROJECT_SOURCE_DIR}/gclpr_${MSYSTEM_NAME}.zip)
list(APPEND ZIP_DEPS ${PROJECT_BINARY_DIR}/changelog.txt ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX})
if(CROSS_GOOS STREQUAL "windows")
    list(APPEND REL_DEPS ${PROJECT_SOURCE_DIR}/gclpr.json)
    list(APPEND ZIP_DEPS ${PROJECT_BINARY_DIR}/gclpr-gui${CMAKE_EXECUTABLE_SUFFIX} ${PROJECT_BINARY_DIR}/npiperelay${CMAKE_EXECUTABLE_SUFFIX})
endif()
foreach(file ${ZIP_DEPS})
    get_filename_component(base ${file} NAME)
    list(APPEND ZIP_CONT ${base})
endforeach()

add_custom_target(release
    DEPENDS ${REL_DEPS}
    COMMAND ${CMAKE_SOURCE_DIR}/cmake/sign.sh ${MSYSTEM_NAME}
    COMMENT "Building release..."
    WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

add_custom_command(OUTPUT ${PROJECT_SOURCE_DIR}/gclpr_${MSYSTEM_NAME}.zip
    DEPENDS ${ZIP_DEPS}
    COMMAND ${CMAKE_COMMAND} -E tar "cfv" ${PROJECT_SOURCE_DIR}/gclpr_${MSYSTEM_NAME}.zip --format=zip ${ZIP_CONT}
    COMMENT "Archiving release..."
    WORKING_DIRECTORY "${PROJECT_BINARY_DIR}")

if(CROSS_GOOS STREQUAL "windows")
    # shortcut
    add_custom_target(bin_gclpr_gui ALL
        DEPENDS ${PROJECT_SOURCE_DIR}/cmd/gui/resources.syso
        BYPRODUCTS ${PROJECT_BINARY_DIR}/gclpr-gui${CMAKE_EXECUTABLE_SUFFIX}
        COMMAND ${GO_ENV} ${GO_EXECUTABLE} build ${GO_MOD_ARGS} -trimpath -o ${PROJECT_BINARY_DIR}/gclpr-gui${CMAKE_EXECUTABLE_SUFFIX}
            -ldflags='-H=windowsgui'
            gclpr/cmd/gui
        COMMENT "Building gclpr-gui..."
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

    add_custom_command(OUTPUT ${PROJECT_SOURCE_DIR}/cmd/gui/resources.syso
         DEPENDS ${PROJECT_SOURCE_DIR}/cmd/gui/resources.rc
             ${PROJECT_SOURCE_DIR}/cmd/gui/manifest.xml
             ${PROJECT_SOURCE_DIR}/cmd/gui/icon.ico
         COMMAND ${CMAKE_RC_COMPILER} -O coff
             -o ${PROJECT_SOURCE_DIR}/cmd/gui/resources.syso
             -i ${PROJECT_SOURCE_DIR}/cmd/gui/resources.rc
         WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
         COMMENT "Building gclpr-gui resources...")
 endif()

# shortcut
add_custom_target(bin_gclpr ALL
    BYPRODUCTS ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX}
    COMMAND ${GO_ENV} ${GO_EXECUTABLE} build ${GO_MOD_ARGS} -trimpath -o ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX}
        gclpr/cmd/cli
    COMMENT "Building gclpr..."
    WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

########################################################################################################
# Test targets
########################################################################################################

add_custom_target(test_util
    COMMAND ${GO_ENV} ${GO_EXECUTABLE} test ${GO_MOD_ARGS} ${TEST_RUN_ARGS} ./util
    WORKING_DIRECTORY ${PROJECT_SOURCE_DIR}
    COMMENT "Util tests...")

########################################################################################################
# Development
########################################################################################################

if(CMAKE_BUILD_TYPE STREQUAL "Debug")

    ########################################################################################################
    # Go linter
    ########################################################################################################

    add_custom_command(OUTPUT ${PROJECT_BINARY_DIR}/staticcheck
        COMMAND GOPATH=${GO_PATH} GOOS= GOARCH= ${GO_EXECUTABLE} build ${GO_MOD_ARGS} -o ${PROJECT_BINARY_DIR}/staticcheck
            honnef.co/go/tools/cmd/staticcheck
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        COMMENT "Building staticcheck executable...")

    add_custom_target(lint
        DEPENDS ${PROJECT_BINARY_DIR}/staticcheck
        COMMAND ${PROJECT_BINARY_DIR}/staticcheck -f stylish -tests=false ./...
        COMMENT "Linting project..."
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

    ########################################################################################################
    # Go dependency management
    ########################################################################################################

    add_custom_target(deps_tidy
        SOURCES ${PROJECT_SOURCE_DIR}/go.mod
        COMMAND ${GO_ENV} ${GO_EXECUTABLE} mod tidy -compat=${GO_MIN_REQURED_VERSION}
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        COMMENT "Cleaning dependencies (go mod tidy)"
        VERBATIM)

    add_custom_target(deps_download
        SOURCES ${PROJECT_SOURCE_DIR}/go.mod
        COMMAND ${GO_ENV} ${GO_EXECUTABLE} mod download all
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        COMMENT "Downloading dependencies (go mod download)"
        VERBATIM)

    add_custom_target(deps_vendor
        SOURCES ${PROJECT_SOURCE_DIR}/go.mod
        COMMAND ${GO_ENV} ${GO_EXECUTABLE} mod vendor
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        COMMENT "Creating vendor directory (go mod vendor)"
        VERBATIM)
endif()
