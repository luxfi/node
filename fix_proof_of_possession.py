import re

# Read the file
with open('vms/platformvm/vm_regression_test.go', 'r') as f:
    content = f.read()

# Pattern to find NewProofOfPossession calls
pattern = r'(\s+)(signer\.NewProofOfPossession\(sk[0-9]*\))(,)'

# Find all matches
matches = list(re.finditer(pattern, content))

# Process matches in reverse order to maintain correct positions
for match in reversed(matches):
    indent = match.group(1)
    call = match.group(2)
    sk_name = re.search(r'\(sk[0-9]*\)', call).group(0)[1:-1]  # Extract sk1, sk2, etc.
    
    # Create replacement with error handling
    replacement = f'''pop{sk_name[-1]}, err := {call}
{indent}require.NoError(err)
{indent}utx, err := builder.NewAddPermissionlessValidatorTx(
{indent}\t&txs.NetValidator{{
{indent}\t\tValidator: txs.Validator{{
{indent}\t\t\tNodeID: ids.GenerateTestNodeID(),
{indent}\t\t\tStart:  uint64(defaultValidateStartTime.Unix()),
{indent}\t\t\tEnd:    uint64(defaultValidateEndTime.Unix()),
{indent}\t\t\tWght:   vm.MinValidatorStake,
{indent}\t\t}},
{indent}\t\tNet: constants.PrimaryNetworkID,
{indent}\t}},
{indent}\tpop{sk_name[-1]}'''
    
    # Find the context around each occurrence to fix properly
    start = match.start()
    # Find the start of the function call (going backwards)
    i = start - 1
    while i >= 0 and 'NewAddPermissionlessValidatorTx' not in content[i:start]:
        i -= 100
    
    # Just replace the signer.NewProofOfPossession line
    # We'll need to do this more carefully
    
print("Manual fixes needed at lines: 1807, 1941, 2141, 2355")
print("Use pattern: pop, err := signer.NewProofOfPossession(sk)")
print("Then add: require.NoError(err)")
