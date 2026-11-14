/*	mempak : N64 controller pak tools for raphnet adapters
	Based on gcn64tools, Copyright (C) 2007-2017  Raphael Assenat <raph@raphnet.net>

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License, or
	(at your option) any later version.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

/* 'mempak adapter ...' : every command that talks to the raphnet adapter.
 * Logic ported from gcn64ctl (main.c) and the GUI's mempak handling. */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>
#include <unistd.h>
#include "cli.h"
#include "raphnetadapter.h"
#include "requests.h"
#include "mempak.h"
#include "mempak_gcn64usb.h"
#include "mempak_fill.h"

#define ARRAY_SIZE(a) (sizeof(a)/sizeof(a[0]))
#define SERIAL_ARG_MAX	128

/* Options shared by every command that opens an adapter. */
struct adapter_opts {
	const char *serial;
	const char *channel_str;
	int channel;
	int verbose;
	int yes;
	int help;
};

#define ADAPTER_COMMON_OPTS(o) \
	{ 's', "serial", &(o).serial, NULL }, \
	{ 'c', "channel", &(o).channel_str, NULL }, \
	{ 'v', "verbose", NULL, &(o).verbose }, \
	{ 'h', "help", NULL, &(o).help }

static void print_common_opts(int with_yes)
{
	printf("  -s, --serial SERIAL  Use the adapter with this serial (default: first adapter found)\n");
	printf("  -c, --channel N      Controller port to use, starting at 0 (default: 0)\n");
	if (with_yes) {
		printf("  -y, --yes            Do not ask for confirmation\n");
	}
	printf("  -v, --verbose        Verbose USB/adapter output\n");
	printf("  -h, --help           Show this help\n");
}

static int is_n64_controller(int type)
{
	return type == CTL_TYPE_N64 || type == CTL_TYPE_N64_NEW;
}

/****************************** helpers *******************************/

static int progress_cb(int addr, void *ctx)
{
	static int is_tty = -1;

	if (is_tty < 0) {
		is_tty = isatty(fileno(stderr));
	}
	if (is_tty) {
		int done = addr + 0x20;
		fprintf(stderr, "\r%s: 0x%04x / 0x%04x (%3d%%)", (const char *)ctx, done, MEMPAK_MEM_SIZE,
			done * 100 / MEMPAK_MEM_SIZE);
		fflush(stderr);
	}
	return 0;
}

static void progress_done(void)
{
	if (isatty(fileno(stderr))) {
		fputc('\n', stderr);
	}
}

static const char *transfer_error(int res)
{
	switch (res) {
		case -1: return "No mempak detected (is a controller with a pak plugged in?)";
		case -2: return "I/O error communicating with the pak";
		case -4: return "Cancelled";
		default: return "Unexpected error";
	}
}

/* Open the adapter selected by serial (or the first usable one) and validate
 * the channel. Returns NULL (with an error printed) on failure. */
static rnt_hdl_t open_adapter(struct adapter_opts *o)
{
	struct rnt_adap_list_ctx *listctx;
	struct rnt_adap_info inf;
	struct rnt_adap_info *found = NULL;
	wchar_t target_serial[SERIAL_ARG_MAX];
	rnt_hdl_t hdl;
	int n_channels;

	if (o->serial) {
		if (mbstowcs(target_serial, o->serial, SERIAL_ARG_MAX) == (size_t)-1) {
			fprintf(stderr, "Invalid serial number '%s'\n", o->serial);
			return NULL;
		}
		target_serial[SERIAL_ARG_MAX-1] = 0;
	}

	o->channel = 0;
	if (o->channel_str && cli_parse_uint("channel", o->channel_str, 255, &o->channel)) {
		return NULL;
	}

	rnt_init(o->verbose);

	listctx = rnt_allocListCtx();
	if (!listctx) {
		fprintf(stderr, "Could not allocate device list context\n");
		return NULL;
	}

	while (rnt_listDevices(&inf, listctx)) {
		if (o->serial) {
			if (0 == wcscmp(inf.str_serial, target_serial)) {
				found = &inf;
				break;
			}
		} else if (!inf.legacy_adapter) {
			found = &inf;
			break;
		}
	}
	rnt_freeListCtx(listctx);

	if (!found) {
		if (o->serial) {
			fprintf(stderr, "No adapter with serial '%s' found. Try 'mempak adapter detect'.\n", o->serial);
		} else {
			fprintf(stderr, "No adapter found. Is it plugged in? Try 'mempak adapter detect'.\n");
		}
		return NULL;
	}

	if (found->legacy_adapter) {
		fprintf(stderr, "Adapter '%ls' is a legacy model and does not support mempak access.\n", found->str_prodname);
		return NULL;
	}

	n_channels = found->caps.n_channels ? found->caps.n_channels : 1;
	if (o->channel >= n_channels) {
		fprintf(stderr, "Channel %d out of range: adapter '%ls' has %d port(s) (0-%d)\n",
			o->channel, found->str_prodname, n_channels, n_channels - 1);
		return NULL;
	}

	hdl = rnt_openDevice(found);
	if (!hdl) {
		fprintf(stderr, "Error opening adapter '%ls' (serial %ls).", found->str_prodname, found->str_serial);
#ifdef __linux__
		fprintf(stderr, " Do you have permissions? See scripts/10-raphnet.rules.");
#endif
		fprintf(stderr, "\n");
		return NULL;
	}

	fprintf(stderr, "Using adapter '%ls' (serial %ls), channel %d\n",
		found->str_prodname, found->str_serial, o->channel);

	/* The adapter would otherwise keep polling the controller between our
	 * pak accesses. */
	rnt_suspendPolling(hdl, 1);

	return hdl;
}

static void close_adapter(rnt_hdl_t hdl)
{
	if (hdl) {
		rnt_suspendPolling(hdl, 0);
		rnt_closeDevice(hdl);
	}
	rnt_shutdown();
}

/****************************** detect ********************************/

static void usage_detect(void)
{
	printf("Usage: mempak adapter detect [options]\n");
	printf("\n");
	printf("List connected raphnet adapters, their firmware, and for each port\n");
	printf("the connected controller and whether a mempak is present.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -v, --verbose        Verbose USB/adapter output\n");
	printf("  -h, --help           Show this help\n");
}

static void detect_channel(rnt_hdl_t hdl, const struct rnt_adap_info *inf, int chn)
{
	int type = -1;
	int probe_pak = 1;

	printf("    Port %d: ", chn);

	if (inf->caps.features & RNTF_CONTROLLER_TYPE) {
		type = rnt_getControllerType(hdl, chn);
		if (type >= 0) {
			printf("%s", rnt_controllerName(type));
			probe_pak = is_n64_controller(type);
		} else {
			printf("Unknown controller");
		}
	} else {
		printf("Controller type not reported by this firmware");
	}

	if (probe_pak) {
		rnt_suspendPolling(hdl, 1);
		printf(", mempak: %s", gcn64lib_mempak_detect(hdl, chn) == 0 ? "present" : "not detected");
		rnt_suspendPolling(hdl, 0);
	}
	printf("\n");
}

static int cmd_detect(int argc, char **argv)
{
	struct adapter_opts o = { 0 };
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos, n_found = 0, chn, n_channels;
	struct rnt_adap_list_ctx *listctx;
	struct rnt_adap_info inf;
	const cli_opt_t opts[] = {
		{ 'v', "verbose", NULL, &o.verbose },
		{ 'h', "help", NULL, &o.help },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (o.help) {
		usage_detect();
		return 0;
	}
	if (n_pos != 0) {
		usage_detect();
		return 2;
	}

	rnt_init(o.verbose);

	listctx = rnt_allocListCtx();
	if (!listctx) {
		fprintf(stderr, "Could not allocate device list context\n");
		return 1;
	}

	while (rnt_listDevices(&inf, listctx)) {
		rnt_hdl_t hdl;
		char version[64];

		n_found++;
		n_channels = inf.caps.n_channels ? inf.caps.n_channels : 1;

		printf("Adapter %d: %ls\n", n_found, inf.str_prodname);
		printf("  Serial:   %ls\n", inf.str_serial);
		printf("  USB ID:   %04x:%04x\n", inf.usb_vid, inf.usb_pid);
		if (o.verbose) {
			printf("  Path:     %s\n", inf.str_path);
		}

		if (inf.legacy_adapter) {
			printf("  Firmware: %d.%d (legacy adapter, mempak access not supported)\n",
				inf.version_major, inf.version_minor);
			continue;
		}

		hdl = rnt_openDevice(&inf);
		if (!hdl) {
			printf("  Status:   could not open device");
#ifdef __linux__
			printf(" (permissions? see scripts/10-raphnet.rules)");
#endif
			printf("\n");
			continue;
		}

		if (0 == rnt_getVersion(hdl, version, sizeof(version))) {
			printf("  Firmware: %s\n", version);
		}
		printf("  Ports:    %d\n", n_channels);
		printf("  Block IO: %s\n", inf.caps.features & RNTF_BLOCK_IO ? "yes" : "no");

		for (chn = 0; chn < n_channels; chn++) {
			detect_channel(hdl, &inf, chn);
		}

		rnt_closeDevice(hdl);
	}
	rnt_freeListCtx(listctx);
	rnt_shutdown();

	if (n_found == 0) {
		printf("No adapter found.\n");
		return 1;
	}

	return 0;
}

/******************************* read *********************************/

static void usage_read(void)
{
	printf("Usage: mempak adapter read [options] [OUTFILE]\n");
	printf("\n");
	printf("Read the whole mempak and save it to OUTFILE.\n");
	printf("Without OUTFILE, a hex dump is printed to stdout.\n");
	printf("\n");
	printf("Options:\n");
	printf("  -f, --format FMT     Output format (default: from extension, else %s)\n", DEFAULT_FORMAT_STR);
	print_common_opts(0);
	printf("\n");
	cli_print_formats();
}

static int cmd_read(int argc, char **argv)
{
	struct adapter_opts o = { 0 };
	const char *format = NULL;
	const char *outfile = NULL;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos, fmt = MPK_FORMAT_INVALID, res, retval = 1;
	mempak_structure_t *pak = NULL;
	rnt_hdl_t hdl;
	const cli_opt_t opts[] = {
		ADAPTER_COMMON_OPTS(o),
		{ 'f', "format", &format, NULL },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (o.help) {
		usage_read();
		return 0;
	}
	if (n_pos > 1) {
		usage_read();
		return 2;
	}
	if (n_pos == 1) {
		outfile = pos[0];
		fmt = cli_resolve_format(format, outfile);
		if (fmt == MPK_FORMAT_INVALID)
			return 2;
	}

	hdl = open_adapter(&o);
	if (!hdl) {
		rnt_shutdown();
		return 1;
	}

	res = gcn64lib_mempak_download(hdl, o.channel, &pak, progress_cb, "Reading");
	progress_done();
	close_adapter(hdl);

	if (res) {
		fprintf(stderr, "Error: %s\n", transfer_error(res));
		return 1;
	}

	if (!outfile) {
		mempak_hexdump(pak);
		retval = 0;
	} else if (0 == mempak_saveToFile(pak, outfile, fmt)) {
		fprintf(stderr, "Wrote '%s' (%s format)\n", outfile, mempak_format2string(fmt));
		if (0 != validate_mempak(pak)) {
			fprintf(stderr, "Warning: the pak contents are not a valid mempak filesystem (unformatted or corrupted)\n");
		}
		retval = 0;
	} else {
		fprintf(stderr, "Error writing file '%s'\n", outfile);
	}

	mempak_free(pak);
	return retval;
}

/******************************* write ********************************/

static void usage_write(void)
{
	printf("Usage: mempak adapter write [options] INFILE\n");
	printf("\n");
	printf("Write a mempak image (.mpk/.n64) to the mempak, replacing all its contents.\n");
	printf("\n");
	printf("Options:\n");
	printf("  --verify             Read the pak back afterwards and compare\n");
	print_common_opts(1);
}

static int verify_pak(rnt_hdl_t hdl, int channel, const mempak_structure_t *expected)
{
	mempak_structure_t *readback = NULL;
	int res, addr;

	res = gcn64lib_mempak_download(hdl, channel, &readback, progress_cb, "Verifying");
	progress_done();
	if (res) {
		fprintf(stderr, "Error: %s\n", transfer_error(res));
		return -1;
	}

	for (addr = 0; addr < MEMPAK_MEM_SIZE; addr++) {
		if (readback->data[addr] != expected->data[addr]) {
			fprintf(stderr, "Verify failed: first mismatch at 0x%04x (wrote 0x%02x, read 0x%02x)\n",
				addr, expected->data[addr], readback->data[addr]);
			mempak_free(readback);
			return -1;
		}
	}

	mempak_free(readback);
	fprintf(stderr, "Verify OK\n");
	return 0;
}

static int cmd_write(int argc, char **argv)
{
	struct adapter_opts o = { 0 };
	int verify = 0;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos, res, retval = 1;
	mempak_structure_t *pak;
	rnt_hdl_t hdl;
	const cli_opt_t opts[] = {
		ADAPTER_COMMON_OPTS(o),
		{ 'y', "yes", NULL, &o.yes },
		{ 0, "verify", NULL, &verify },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (o.help) {
		usage_write();
		return 0;
	}
	if (n_pos != 1) {
		usage_write();
		return 2;
	}

	pak = mempak_loadFromFile(pos[0]);
	if (!pak) {
		fprintf(stderr, "Could not load mempak file '%s'\n", pos[0]);
		return 1;
	}
	if (0 != validate_mempak(pak)) {
		fprintf(stderr, "Warning: '%s' is not a valid mempak filesystem (unformatted or corrupted)\n", pos[0]);
	}

	hdl = open_adapter(&o);
	if (!hdl) {
		mempak_free(pak);
		rnt_shutdown();
		return 1;
	}

	if (!o.yes && !cli_confirm("This will overwrite ALL data on the mempak. Continue?")) {
		fprintf(stderr, "Cancelled\n");
		goto done;
	}

	res = gcn64lib_mempak_upload(hdl, o.channel, pak, progress_cb, "Writing");
	progress_done();
	if (res) {
		fprintf(stderr, "Error: %s\n", transfer_error(res));
		goto done;
	}
	fprintf(stderr, "Wrote '%s' to mempak\n", pos[0]);

	if (verify && verify_pak(hdl, o.channel, pak)) {
		goto done;
	}

	retval = 0;
done:
	close_adapter(hdl);
	mempak_free(pak);
	return retval;
}

/******************************* erase ********************************/

static void usage_erase(void)
{
	printf("Usage: mempak adapter erase [options]\n");
	printf("\n");
	printf("Erase the mempak. By default a blank, formatted filesystem is written so\n");
	printf("the pak is immediately usable by games.\n");
	printf("\n");
	printf("Options:\n");
	printf("  --fill-ff            Instead, overwrite every byte with 0xFF and verify\n");
	printf("                       (raw wipe; the pak will need formatting before use)\n");
	print_common_opts(1);
}

static int cmd_erase(int argc, char **argv)
{
	struct adapter_opts o = { 0 };
	int fill_ff = 0;
	char *pos[CLI_MAX_POSITIONAL];
	int n_pos, res, retval = 1;
	mempak_structure_t *pak = NULL;
	rnt_hdl_t hdl;
	const cli_opt_t opts[] = {
		ADAPTER_COMMON_OPTS(o),
		{ 'y', "yes", NULL, &o.yes },
		{ 0, "fill-ff", NULL, &fill_ff },
	};

	if (cli_parse(argc, argv, opts, ARRAY_SIZE(opts), pos, &n_pos))
		return 2;
	if (o.help) {
		usage_erase();
		return 0;
	}
	if (n_pos != 0) {
		usage_erase();
		return 2;
	}

	hdl = open_adapter(&o);
	if (!hdl) {
		rnt_shutdown();
		return 1;
	}

	if (!o.yes && !cli_confirm("This will erase ALL data on the mempak. Continue?")) {
		fprintf(stderr, "Cancelled\n");
		goto done;
	}

	if (fill_ff) {
		if (gcn64lib_mempak_detect(hdl, o.channel)) {
			fprintf(stderr, "Error: %s\n", transfer_error(-1));
			goto done;
		}
		/* Confirmation already handled above. */
		if (mempak_fill(hdl, o.channel, 0xFF, 1, NULL)) {
			goto done;
		}
		fprintf(stderr, "Mempak filled with 0xFF\n");
	} else {
		pak = mempak_new();
		if (!pak)
			goto done;

		res = gcn64lib_mempak_upload(hdl, o.channel, pak, progress_cb, "Erasing");
		progress_done();
		if (res) {
			fprintf(stderr, "Error: %s\n", transfer_error(res));
			goto done;
		}
		if (verify_pak(hdl, o.channel, pak)) {
			goto done;
		}
		fprintf(stderr, "Mempak erased (blank formatted filesystem written)\n");
	}

	retval = 0;
done:
	close_adapter(hdl);
	if (pak)
		mempak_free(pak);
	return retval;
}

/****************************** dispatch ******************************/

static void usage_adapter(void)
{
	printf("Usage: mempak adapter COMMAND [options]\n");
	printf("\n");
	printf("Commands that talk to a raphnet adapter:\n");
	printf("  detect    List adapters, ports, controllers and mempak presence\n");
	printf("  read      Read the mempak to an image file\n");
	printf("  write     Write an image file to the mempak\n");
	printf("  erase     Erase the mempak (blank formatted, or --fill-ff)\n");
	printf("\n");
	printf("Run 'mempak adapter COMMAND --help' for details.\n");
}

int cmd_adapter(int argc, char **argv)
{
	const char *sub;

	if (argc < 2) {
		usage_adapter();
		return 2;
	}

	sub = argv[1];
	argc--;
	argv++;

	if (0 == strcmp(sub, "detect"))
		return cmd_detect(argc, argv);
	if (0 == strcmp(sub, "read"))
		return cmd_read(argc, argv);
	if (0 == strcmp(sub, "write"))
		return cmd_write(argc, argv);
	if (0 == strcmp(sub, "erase"))
		return cmd_erase(argc, argv);
	if (0 == strcmp(sub, "help") || 0 == strcmp(sub, "-h") || 0 == strcmp(sub, "--help")) {
		usage_adapter();
		return 0;
	}

	fprintf(stderr, "Unknown adapter command '%s'\n\n", sub);
	usage_adapter();
	return 2;
}
