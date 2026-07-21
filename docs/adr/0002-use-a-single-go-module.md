# Use a single Go module for the core and built-in Providers

The core SMS contract and all five built-in Provider packages ship from one Go module so they share one version, test workflow, and documentation set. This keeps the initial release operationally simple; consumers only compile Provider packages they import, while accepting that the module declares all five SDK dependencies. Separate Provider modules can be considered later if dependency-graph cost proves material enough to justify multi-module release complexity.
