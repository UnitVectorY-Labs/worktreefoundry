This is a command line application written in Go. It uses minimal dependencies prefering to rely on the Go standard library as much as possible. However, it may use third party libraries for specific functionality not available in the standard library.

The different functionality for this application are implemented as subcommands each providing the specific functions for the application.

The main "worktreefoundry web" command hosts a web server that provides a user interface.  This can be run locally and must point to a --repository.

The implementation philosophy for this application is radical simplicity. Only Go templates, HTMX, CSS, and JavaScript are used for the web interface. No complex front end frameworks are used. This is meant to be an application with minimal complexity and a simple interface. HTMX provides the foundation for a dynamic web interface without the complexity of a front end framework.

All command line parameters can be provided via environment variables as well. See the individual command help for details on which parameters are supported.

When testing locally, a new git repository can be initialized in /tmp with a random name and used for testing.  An initialization command provided by the application should be made available to not only initialize the repository but also to populate it with some sample schema as the starting point for all applications that will be perfect for testing.

If playwright is available use it for testing of the web interface when performing development.