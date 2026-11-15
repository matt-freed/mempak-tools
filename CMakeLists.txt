cmake_minimum_required(VERSION 3.16)

project(mempak VERSION 0.1.0 LANGUAGES C)

if(NOT CMAKE_BUILD_TYPE AND NOT CMAKE_CONFIGURATION_TYPES)
	set(CMAKE_BUILD_TYPE Release CACHE STRING "Build type" FORCE)
endif()

# Version reported by 'mempak --version'. CI sets MEMPAK_VERSION to
# <major>.<minor>.<CI run number>, taking major.minor from project() above.
# Bump the minor (or major) here to start a new release series.
set(MEMPAK_VERSION "${PROJECT_VERSION}" CACHE STRING "Version string reported by the CLI")

set(CMAKE_C_STANDARD 99)
set(CMAKE_C_EXTENSIONS ON) # gnu99: the original sources rely on GNU/POSIX extensions

find_package(Threads REQUIRED)

# ---------------------------------------------------------------------------
# Vendored hidapi (libusb/hidapi, see third_party/hidapi/VERSION)
# ---------------------------------------------------------------------------
set(HIDAPI_DIR ${CMAKE_CURRENT_SOURCE_DIR}/third_party/hidapi)

if(APPLE)
	add_library(hidapi STATIC ${HIDAPI_DIR}/mac/hid.c)
	target_link_libraries(hidapi PRIVATE Threads::Threads
		"-framework IOKit" "-framework CoreFoundation" "-framework AppKit")
elseif(WIN32)
	# windows/hid.c #includes hidapi_descriptor_reconstruct.c itself.
	add_library(hidapi STATIC ${HIDAPI_DIR}/windows/hid.c)
	target_include_directories(hidapi PRIVATE ${HIDAPI_DIR}/windows)
	target_compile_definitions(hidapi PRIVATE _CRT_SECURE_NO_WARNINGS)
elseif(UNIX)
	find_package(PkgConfig REQUIRED)
	pkg_check_modules(libudev REQUIRED IMPORTED_TARGET libudev)
	add_library(hidapi STATIC ${HIDAPI_DIR}/linux/hid.c)
	target_link_libraries(hidapi PRIVATE PkgConfig::libudev Threads::Threads)
else()
	message(FATAL_ERROR "Unsupported platform")
endif()

target_include_directories(hidapi PUBLIC ${HIDAPI_DIR}/hidapi)
set_target_properties(hidapi PROPERTIES C_EXTENSIONS ON)

# ---------------------------------------------------------------------------
# Core library: mempak image handling + raphnet adapter access
# (extracted from gcn64tools rntlib)
# ---------------------------------------------------------------------------
set(CORE_SOURCES
	src/lib/mempak.c
	src/lib/mempak_fs.c
	src/lib/mempak_gcn64usb.c
	src/lib/mempak_fill.c
	src/lib/raphnetadapter.c
	src/lib/gcn64lib.c
	src/lib/hexdump.c
	src/lib/timer.c
	src/lib/uiio.c
)
if(WIN32)
	list(APPEND CORE_SOURCES src/lib/strcasestr.c)
endif()

add_library(mempakcore STATIC ${CORE_SOURCES})
target_include_directories(mempakcore PUBLIC src/lib)
target_link_libraries(mempakcore PUBLIC hidapi)
if(WIN32)
	target_compile_definitions(mempakcore PUBLIC WINDOWS _WIN32_WINNT=0x0600)
endif()

# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
add_executable(mempak
	src/main.c
	src/cli.c
	src/cmd_adapter.c
	src/cmd_offline.c
)
target_link_libraries(mempak PRIVATE mempakcore)
target_compile_definitions(mempak PRIVATE VERSION_STR="${MEMPAK_VERSION}")

if(MINGW)
	# Produce a self-contained .exe (no libgcc / winpthread DLLs needed).
	target_link_options(mempak PRIVATE -static)
endif()

if(CMAKE_C_COMPILER_ID MATCHES "GNU|Clang")
	target_compile_options(mempakcore PRIVATE -Wall)
	target_compile_options(mempak PRIVATE -Wall -Wextra -Wno-unused-parameter)
endif()

install(TARGETS mempak RUNTIME DESTINATION bin)

# ---------------------------------------------------------------------------
# Tests (offline only; no adapter required)
# ---------------------------------------------------------------------------
include(CTest)
if(BUILD_TESTING)
	find_program(BASH_PROGRAM bash)
	if(BASH_PROGRAM)
		add_test(NAME offline
			COMMAND ${BASH_PROGRAM} ${CMAKE_CURRENT_SOURCE_DIR}/tests/offline.sh $<TARGET_FILE:mempak>)
	endif()
endif()
