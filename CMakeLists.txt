cmake_minimum_required(VERSION 3.0)

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
    elseif(MSYSTEM_NAME STREQUAL "darwin")
        set(CROSS_GOOS darwin)
        set(CROSS_GOARCH amd64)
        message(STATUS "Targeting 64 bits Darwin executable")
    else()
        message(FATAL_ERROR "Currently unsuppored environment \"${MSYSTEM_NAME}\". CMake willl exit now.")
    endif()
else()
    if (NOT "$ENV{MSYSTEM_NAME}" STREQUAL "")
        set(MSYSTEM_NAME "$ENV{MSYSTEM_NAME}" CACHE INTERNAL "Copied from environment")
    endif()
    if("$ENV{MSYSTEM_NAME}" STREQUAL "linux_i386")
        set(CROSS_GOOS linux)
        set(CROSS_GOARCH 386)
        message(STATUS "Targeting 32 bits Linux executable")
    else()
        # host platform is always linux_amd64
        message(STATUS "Targeting 64 bits Linux executable")
    endif()
endif()

find_package(Go 1.15 REQUIRED)
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
set(PRJ_VERSION_Minor "1")
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
if(CROSS_GOOS STREQUAL "windows" AND NOT EXISTS "${PROJECT_SOURCE_DIR}/gclpr.json")
    configure_file("${PROJECT_SOURCE_DIR}/cmake/gclpr.json.in" "${PROJECT_SOURCE_DIR}/gclpr.json")
endif()

# distribute history
execute_process(COMMAND ${CMAKE_COMMAND} -E env ${GIT_EXECUTABLE} changelog --all --tag "v${PRJ_VERSION_Major}.${PRJ_VERSION_Minor}.${PRJ_VERSION_Patch}" --stdout OUTPUT_FILE ${PROJECT_BINARY_DIR}/changelog.txt ERROR_QUIET)

message(STATUS "Building version \"${PRJ_VERSION_Major}.${PRJ_VERSION_Minor}.${PRJ_VERSION_Patch}\" git \"${GIT_HASH}\"")

if(CMAKE_BUILD_TYPE STREQUAL "Debug")
    # Create git pre-commit hook to check formatting before commit
    if(EXISTS "${PROJECT_SOURCE_DIR}/.git" AND IS_DIRECTORY "${PROJECT_SOURCE_DIR}/.git")
        execute_process(COMMAND ${CMAKE_COMMAND} -E copy ${PROJECT_SOURCE_DIR}/cmake/pre-commit ${PROJECT_SOURCE_DIR}/.git/hooks/. WORKING_DIRECTORY ${PROJECT_SOURCE_DIR})
    endif()
endif()

########################################################################################################
# Some contexts dependent settings
########################################################################################################

list(APPEND GO_ENV
    GOPATH=${GO_PATH}
    GOOS=${CROSS_GOOS}
    GOARCH=${CROSS_GOARCH}
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
if(CROSS_GOOS STREQUAL "windows")
    list(APPEND REL_DEPS ${PROJECT_SOURCE_DIR}/gclpr.json)
endif()

add_custom_target(release
    DEPENDS ${REL_DEPS}
    COMMAND ${CMAKE_SOURCE_DIR}/cmake/sign.sh ${MSYSTEM_NAME}
    COMMENT "Building release..."
    WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

add_custom_command(OUTPUT ${PROJECT_SOURCE_DIR}/gclpr_${MSYSTEM_NAME}.zip
    DEPENDS ${PROJECT_BINARY_DIR}/changelog.txt
        ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX}
        COMMAND ${CMAKE_COMMAND} -E tar "cfv" ${PROJECT_SOURCE_DIR}/gclpr_${MSYSTEM_NAME}.zip --format=zip changelog.txt gclpr${CMAKE_EXECUTABLE_SUFFIX}
    COMMENT "Archiving release..."
    WORKING_DIRECTORY "${PROJECT_BINARY_DIR}")

# shortcut
add_custom_target(bin_gclpr ALL
    DEPENDS ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX}
    WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

add_custom_command(OUTPUT ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX}
    COMMAND ${GO_ENV} ${GO_EXECUTABLE} build ${GO_MOD_ARGS} -trimpath -o ${PROJECT_BINARY_DIR}/gclpr${CMAKE_EXECUTABLE_SUFFIX}
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

    # Running linters on main target
    find_program(GO_LINTER golangci-lint PATHS ${PROJECT_BINARY_DIR} NO_DEFAULT_PATH)
    if(NOT GO_LINTER)
        set(golangci-lint-ver "1.33.0")
        message(STATUS "Preparing golangci-lint ${golangci-lint-ver}")
        file(DOWNLOAD "https://github.com/golangci/golangci-lint/releases/download/v${golangci-lint-ver}/golangci-lint-${golangci-lint-ver}-linux-amd64.tar.gz" ${PROJECT_BINARY_DIR}/golangci-lint.tar.gz
            INACTIVITY_TIMEOUT 60
            TIMEOUT 300
            STATUS DOWNLOAD_RES
            EXPECTED_HASH SHA256=e2d6082f1df53c5d2c280765000f9e82783ea909ba419c6c4e172936b076031e)
        list(GET DOWNLOAD_RES 0 RES)
        list(GET DOWNLOAD_RES 1 MSG)
        if(RES)
            message(FATAL_ERROR "${MSG} : ${RES}")
        endif()
        execute_process(COMMAND ${CMAKE_COMMAND} -E tar xf golangci-lint.tar.gz WORKING_DIRECTORY ${PROJECT_BINARY_DIR})
        execute_process(COMMAND ${CMAKE_COMMAND} -E copy golangci-lint-${golangci-lint-ver}-linux-amd64/golangci-lint golangci-lint WORKING_DIRECTORY ${PROJECT_BINARY_DIR})
        execute_process(COMMAND ${CMAKE_COMMAND} -E remove_directory golangci-lint-${golangci-lint-ver}-linux-amd64 WORKING_DIRECTORY ${PROJECT_BINARY_DIR})
        execute_process(COMMAND ${CMAKE_COMMAND} -E remove golangci-lint.tar.gz WORKING_DIRECTORY ${PROJECT_BINARY_DIR})
        set(GO_LINTER ${PROJECT_BINARY_DIR}/golangci-lint)
    endif()

    add_custom_target(lint
        COMMAND GOPATH=${GO_PATH} GO111MODULE=on GOOS=${CROSS_GOOS} GOARCH=${CROSS_GOARCH} ${GO_LINTER} run
        COMMENT "Mega-linting project with ${GO_LINTER}..."
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}")

    ########################################################################################################
    # Go dependency management
    ########################################################################################################

    add_custom_target(deps_tidy
        SOURCES ${PROJECT_SOURCE_DIR}/go.mod
        COMMAND ${GO_ENV} ${GO_EXECUTABLE} mod tidy
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        COMMENT "Cleaning dependencies (go mod tidy)"
        VERBATIM)

    add_custom_target(deps_vendor
        SOURCES ${PROJECT_SOURCE_DIR}/go.mod
        COMMAND ${GO_ENV} ${GO_EXECUTABLE} mod vendor
        WORKING_DIRECTORY "${PROJECT_SOURCE_DIR}"
        COMMENT "Creating vendor directory (go mod vendor)"
        VERBATIM)
endif()
