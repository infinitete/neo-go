package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/infinitete/neo-go/config"
	"github.com/infinitete/neo-go/pkg/core"
	"github.com/infinitete/neo-go/pkg/core/storage"
	"github.com/infinitete/neo-go/pkg/io"
	"github.com/infinitete/neo-go/pkg/network"
	"github.com/infinitete/neo-go/pkg/network/metrics"
	"github.com/infinitete/neo-go/pkg/rpc"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

// NewCommands returns 'node' command.
func NewCommands() []cli.Command {
	var cfgFlags = []cli.Flag{
		cli.StringFlag{Name: "config-path"},
		cli.BoolFlag{Name: "privnet, p"},
		cli.BoolFlag{Name: "mainnet, m"},
		cli.BoolFlag{Name: "testnet, t"},
		cli.BoolFlag{Name: "debug, d"},
	}
	var cfgWithCountFlags = make([]cli.Flag, len(cfgFlags))
	copy(cfgWithCountFlags, cfgFlags)
	cfgWithCountFlags = append(cfgWithCountFlags,
		cli.UintFlag{
			Name:  "count, c",
			Usage: "number of blocks to be processed (default or 0: all chain)",
		},
		cli.UintFlag{
			Name:  "skip, s",
			Usage: "number of blocks to skip (default: 0)",
		},
	)
	var cfgCountOutFlags = make([]cli.Flag, len(cfgWithCountFlags))
	copy(cfgCountOutFlags, cfgWithCountFlags)
	cfgCountOutFlags = append(cfgCountOutFlags, cli.StringFlag{
		Name:  "out, o",
		Usage: "Output file (stdout if not given)",
	})
	var cfgCountInFlags = make([]cli.Flag, len(cfgWithCountFlags))
	copy(cfgCountInFlags, cfgWithCountFlags)
	cfgCountInFlags = append(cfgCountInFlags, cli.StringFlag{
		Name:  "in, i",
		Usage: "Input file (stdin if not given)",
	})
	return []cli.Command{
		{
			Name:   "node",
			Usage:  "start a NEO node",
			Action: startServer,
			Flags:  cfgFlags,
		},
		{
			Name:  "db",
			Usage: "database manipulations",
			Subcommands: []cli.Command{
				{
					Name:   "dump",
					Usage:  "dump blocks (starting with block #1) to the file",
					Action: dumpDB,
					Flags:  cfgCountOutFlags,
				},
				{
					Name:   "restore",
					Usage:  "restore blocks from the file",
					Action: restoreDB,
					Flags:  cfgCountInFlags,
				},
			},
		},
	}
}

func newGraceContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		cancel()
	}()
	return ctx
}

// getConfigFromContext looks at path and mode flags in the given config and
// returns appropriate config.
func getConfigFromContext(ctx *cli.Context) (config.Config, error) {
	var net = config.ModePrivNet
	if ctx.Bool("testnet") {
		net = config.ModeTestNet
	}
	if ctx.Bool("mainnet") {
		net = config.ModeMainNet
	}
	configPath := "./config"
	if argCp := ctx.String("config-path"); argCp != "" {
		configPath = argCp
	}
	return config.Load(configPath, net)
}

// handleLoggingParams reads logging parameters.
// If user selected debug level -- function enables it.
// If logPath is configured -- function creates dir and file for logging.
func handleLoggingParams(ctx *cli.Context, cfg config.ApplicationConfiguration) error {
	if ctx.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	}

	if logPath := cfg.LogPath; logPath != "" {
		if err := io.MakeDirForFile(logPath, "logger"); err != nil {
			return err
		}
		f, err := os.Create(logPath)
		if err != nil {
			return err
		}
		log.SetOutput(f)
	}
	return nil
}

func getCountAndSkipFromContext(ctx *cli.Context) (uint32, uint32) {
	count := uint32(ctx.Uint("count"))
	skip := uint32(ctx.Uint("skip"))
	return count, skip
}

func dumpDB(ctx *cli.Context) error {
	cfg, err := getConfigFromContext(ctx)
	if err != nil {
		return cli.NewExitError(err, 1)
	}
	if err := handleLoggingParams(ctx, cfg.ApplicationConfiguration); err != nil {
		return cli.NewExitError(err, 1)
	}
	count, skip := getCountAndSkipFromContext(ctx)

	var outStream = os.Stdout
	if out := ctx.String("out"); out != "" {
		outStream, err = os.Create(out)
		if err != nil {
			return cli.NewExitError(err, 1)
		}
	}
	defer outStream.Close()
	writer := io.NewBinWriterFromIO(outStream)

	grace, cancel := context.WithCancel(newGraceContext())
	defer cancel()

	chain, err := initBlockChain(cfg)
	if err != nil {
		return cli.NewExitError(err, 1)
	}
	go chain.Run(grace)

	chainHeight := chain.BlockHeight()
	if skip+count > chainHeight {
		return cli.NewExitError(fmt.Errorf("chain is not that high (%d) to dump %d blocks starting from %d", chainHeight, count, skip), 1)
	}
	if count == 0 {
		count = chainHeight - skip
	}
	writer.WriteLE(count)
	for i := skip + 1; i <= count; i++ {
		bh := chain.GetHeaderHash(int(i))
		b, err := chain.GetBlock(bh)
		if err != nil {
			return cli.NewExitError(fmt.Errorf("failed to get block %d: %s", i, err), 1)
		}
		b.EncodeBinary(writer)
		if writer.Err != nil {
			return cli.NewExitError(err, 1)
		}
	}
	return nil
}
func restoreDB(ctx *cli.Context) error {
	cfg, err := getConfigFromContext(ctx)
	if err != nil {
		return err
	}
	if err := handleLoggingParams(ctx, cfg.ApplicationConfiguration); err != nil {
		return cli.NewExitError(err, 1)
	}
	count, skip := getCountAndSkipFromContext(ctx)

	var inStream = os.Stdin
	if in := ctx.String("in"); in != "" {
		inStream, err = os.Open(in)
		if err != nil {
			return cli.NewExitError(err, 1)
		}
	}
	defer inStream.Close()
	reader := io.NewBinReaderFromIO(inStream)

	grace, cancel := context.WithCancel(newGraceContext())
	defer cancel()

	chain, err := initBlockChain(cfg)
	if err != nil {
		return err
	}
	go chain.Run(grace)

	var allBlocks uint32
	reader.ReadLE(&allBlocks)
	if reader.Err != nil {
		return cli.NewExitError(err, 1)
	}
	if skip+count > allBlocks {
		return cli.NewExitError(fmt.Errorf("input file has only %d blocks, can't read %d starting from %d", allBlocks, count, skip), 1)
	}
	if count == 0 {
		count = allBlocks
	}
	i := uint32(0)
	for ; i < skip; i++ {
		b := &core.Block{}
		b.DecodeBinary(reader)
		if reader.Err != nil {
			return cli.NewExitError(err, 1)
		}
	}
	for ; i < count; i++ {
		b := &core.Block{}
		b.DecodeBinary(reader)
		if reader.Err != nil {
			return cli.NewExitError(err, 1)
		}
		err := chain.AddBlock(b)
		if err != nil {
			return cli.NewExitError(fmt.Errorf("failed to add block %d: %s", i, err), 1)
		}
	}

	return nil
}

func startServer(ctx *cli.Context) error {
	cfg, err := getConfigFromContext(ctx)
	if err != nil {
		return err
	}
	if err := handleLoggingParams(ctx, cfg.ApplicationConfiguration); err != nil {
		return err
	}

	grace, cancel := context.WithCancel(newGraceContext())
	defer cancel()

	serverConfig := network.NewServerConfig(cfg)

	chain, err := initBlockChain(cfg)
	if err != nil {
		return err
	}

	configureAddresses(cfg.ApplicationConfiguration)
	server := network.NewServer(serverConfig, chain)
	rpcServer := rpc.NewServer(chain, cfg.ApplicationConfiguration.RPC, server)
	errChan := make(chan error)
	monitoring := metrics.NewMetricsService(cfg.ApplicationConfiguration.Monitoring)

	go chain.Run(grace)
	go server.Start(errChan)
	go rpcServer.Start(errChan)
	go monitoring.Start()

	fmt.Println(logo())
	fmt.Println(server.UserAgent)
	fmt.Println()

	var shutdownErr error
Main:
	for {
		select {
		case err := <-errChan:
			shutdownErr = errors.Wrap(err, "Error encountered by server")
			cancel()

		case <-grace.Done():
			server.Shutdown()
			if serverErr := rpcServer.Shutdown(); serverErr != nil {
				shutdownErr = errors.Wrap(serverErr, "Error encountered whilst shutting down server")
			}
			monitoring.ShutDown()
			break Main
		}
	}

	if shutdownErr != nil {
		return cli.NewExitError(shutdownErr, 1)
	}

	return nil
}

// configureAddresses sets up addresses for RPC and Monitoring depending from the provided config.
// In case RPC or Monitoring Address provided each of them will use it.
// In case global Address (of the node) provided and RPC/Monitoring don't have configured addresses they will
// use global one. So Node and RPC and Monitoring will run on one address.
func configureAddresses(cfg config.ApplicationConfiguration) {
	if cfg.Address != "" {
		if cfg.RPC.Address == "" {
			cfg.RPC.Address = cfg.Address
		}
		if cfg.Monitoring.Address == "" {
			cfg.Monitoring.Address = cfg.Address
		}
	}
}

// initBlockChain initializes BlockChain with preselected DB.
func initBlockChain(cfg config.Config) (*core.Blockchain, error) {
	store, err := storage.NewStore(cfg.ApplicationConfiguration.DBConfiguration)
	if err != nil {
		return nil, cli.NewExitError(fmt.Errorf("could not initialize storage: %s", err), 1)
	}

	chain, err := core.NewBlockchain(store, cfg.ProtocolConfiguration)
	if err != nil {
		return nil, cli.NewExitError(fmt.Errorf("could not initialize blockchain: %s", err), 1)
	}
	return chain, nil
}

func logo() string {
	return `
    _   ____________        __________
   / | / / ____/ __ \      / ____/ __ \
  /  |/ / __/ / / / /_____/ / __/ / / /
 / /|  / /___/ /_/ /_____/ /_/ / /_/ /
/_/ |_/_____/\____/      \____/\____/
`
}
