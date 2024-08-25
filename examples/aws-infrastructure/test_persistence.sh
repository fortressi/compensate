#!/bin/bash
# Test script for the persistent saga CLI

set -e

echo "🧪 Testing Persistent Saga CLI"
echo "=============================="

# Clean up any existing state
rm -rf ./test-saga-state

# Test 1: Deploy with auto-generated ID
echo ""
echo "Test 1: Deploy with auto-generated saga ID..."
./aws-infrastructure deploy --state-dir ./test-saga-state --region us-east-1 2>&1 | tee deploy1.log

# Extract the saga ID from the output
SAGA_ID=$(grep -oE 'saga ID: vpc-[a-f0-9-]+' deploy1.log | cut -d' ' -f3)
echo "Created saga with ID: $SAGA_ID"

# Test 2: List sagas
echo ""
echo "Test 2: List sagas..."
./aws-infrastructure list ./test-saga-state

# Test 3: Try to deploy with same ID (should fail)
echo ""
echo "Test 3: Try to deploy with duplicate saga ID (should fail)..."
if ./aws-infrastructure deploy --state-dir ./test-saga-state --saga-id "$SAGA_ID" 2>&1; then
    echo "ERROR: Deploy with duplicate ID should have failed!"
    exit 1
else
    echo "✓ Correctly rejected duplicate saga ID"
fi

# Test 4: Deploy another saga
echo ""
echo "Test 4: Deploy second saga..."
./aws-infrastructure deploy --state-dir ./test-saga-state --saga-id "vpc-test-2" --region us-west-2

# Test 5: List all sagas
echo ""
echo "Test 5: List all sagas..."
./aws-infrastructure list ./test-saga-state

# Test 6: Destroy first saga
echo ""
echo "Test 6: Destroy first saga..."
./aws-infrastructure destroy --state-dir ./test-saga-state --saga-id "$SAGA_ID"

# Test 7: List sagas after destroy
echo ""
echo "Test 7: List sagas after destroy..."
./aws-infrastructure list ./test-saga-state

# Test 8: Try to destroy non-existent saga
echo ""
echo "Test 8: Try to destroy non-existent saga (should fail)..."
if ./aws-infrastructure destroy --state-dir ./test-saga-state --saga-id "vpc-does-not-exist" 2>&1; then
    echo "ERROR: Destroy of non-existent saga should have failed!"
    exit 1
else
    echo "✓ Correctly reported saga not found"
fi

# Clean up
echo ""
echo "Cleaning up test state..."
./aws-infrastructure destroy --state-dir ./test-saga-state --saga-id "vpc-test-2"
rm -rf ./test-saga-state deploy1.log

echo ""
echo "✅ All tests passed!"